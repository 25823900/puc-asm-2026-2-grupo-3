#!/usr/bin/env bash
# Smoke test de ponta a ponta contra o stack já no ar (docker compose up -d --wait).
# Percorre o fluxo do contrato: criar link → redirecionar → LinkClicked no Kafka →
# stats-service grava → BFF mostra o clique; depois posse (bob vê 404) e exclusão.
#
#   scripts/smoke-e2e.sh
#
# Requer bash, curl e jq. Endereços configuráveis por BFF_URL e KEYCLOAK_URL.
set -euo pipefail

BFF_URL="${BFF_URL:-http://localhost:3000}"
KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8080}"
POLL_TIMEOUT_SECONDS="${POLL_TIMEOUT_SECONDS:-30}"

for cmd in curl jq; do
  command -v "$cmd" > /dev/null || { echo "ERRO: '$cmd' não encontrado no PATH" >&2; exit 1; }
done

body_file="$(mktemp)"
link_id=""

cleanup() {
  # Se o teste parou no meio, tenta apagar o link criado para não sujar a listagem.
  if [[ -n "$link_id" ]]; then
    curl -s -o /dev/null -X DELETE -H "Authorization: Bearer $token_alice" \
      "$BFF_URL/api/links/$link_id" || true
  fi
  rm -f "$body_file"
}
trap cleanup EXIT

step() { echo "==> $*"; }
fail() { echo "FALHOU: $*" >&2; [[ -s "$body_file" ]] && cat "$body_file" >&2 && echo >&2; exit 1; }

# jq sem o \r que o jq do Windows acrescenta no fim da linha.
json() { jq -r "$@" < "$body_file" | tr -d '\r'; }

# request MÉTODO CAMINHO TOKEN [CORPO]: grava o corpo em $body_file e imprime o status.
request() {
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-s -o "$body_file" -w '%{http_code}' -X "$method")
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' --data "$body")
  curl "${args[@]}" "$BFF_URL$path"
}

expect_status() {
  local expected="$1" actual="$2" what="$3"
  [[ "$actual" == "$expected" ]] || fail "$what: esperado $expected, recebido $actual"
}

get_token() {
  curl -s --fail \
    -d grant_type=password -d client_id=encurta-postman \
    -d "username=$1" -d "password=$2" \
    "$KEYCLOAK_URL/realms/encurta/protocol/openid-connect/token" \
    | jq -r .access_token | tr -d '\r'
}

step "1. Tokens da alice e do bob"
token_alice="$(get_token alice alice123)" || fail "token da alice"
token_bob="$(get_token bob bob123)" || fail "token do bob"
[[ -n "$token_alice" && "$token_alice" != null ]] || fail "token da alice vazio"
[[ -n "$token_bob" && "$token_bob" != null ]] || fail "token do bob vazio"

step "2. POST /api/links"
target_url="https://example.com/smoke-e2e/$(date +%s)"
status="$(request POST /api/links "$token_alice" "{\"targetUrl\":\"$target_url\"}")"
expect_status 201 "$status" "POST /api/links"
link_id="$(json .id)"
slug="$(json .slug)"
[[ "$slug" =~ ^[0-9a-zA-Z]{7}$ ]] || fail "slug fora do formato: $slug"
[[ "$(json .targetUrl)" == "$target_url" ]] || fail "targetUrl diferente do enviado"
[[ "$(json .shortUrl)" == */r/"$slug" ]] || fail "shortUrl não termina em /r/$slug"
[[ "$(json 'has("ownerId")')" == false ]] || fail "a resposta pública expõe ownerId"
echo "    link $link_id, slug $slug"

step "3. GET /r/$slug redireciona"
redirect="$(curl -s -o /dev/null -w '%{http_code} %{redirect_url}' "$BFF_URL/r/$slug")"
status="${redirect%% *}"
location="${redirect#* }"
expect_status 302 "$status" "GET /r/$slug"
[[ "$location" == "$target_url" ]] || fail "Location '$location' diferente de '$target_url'"

step "4. GET /api/links até totalClicks == 1 (até ${POLL_TIMEOUT_SECONDS} s)"
deadline=$((SECONDS + POLL_TIMEOUT_SECONDS))
while :; do
  status="$(request GET /api/links "$token_alice")"
  expect_status 200 "$status" "GET /api/links"
  total="$(json --arg id "$link_id" '.[] | select(.id == $id) | .totalClicks')"
  [[ "$total" == 1 ]] && break
  ((SECONDS < deadline)) || fail "totalClicks não chegou a 1 (último valor: '${total:-link ausente}')"
  sleep 1
done

step "5. GET /api/links/$link_id/stats"
today="$(date -u +%F)"
status="$(request GET "/api/links/$link_id/stats" "$token_alice")"
expect_status 200 "$status" "GET /api/links/{id}/stats"
[[ "$(json .totalClicks)" == 1 ]] || fail "totalClicks diferente de 1"
[[ "$(json -c .clicksPerDay)" == "[{\"date\":\"$today\",\"clicks\":1}]" ]] \
  || fail "clicksPerDay diferente de 1 clique em $today"

step "6. O bob não vê o link da alice"
status="$(request GET "/api/links/$link_id/stats" "$token_bob")"
expect_status 404 "$status" "stats do link da alice com o token do bob"
[[ "$(json .error.code)" == LINK_NOT_FOUND ]] || fail "código de erro diferente de LINK_NOT_FOUND"

step "7. DELETE /api/links/$link_id e o slug deixa de redirecionar"
status="$(request DELETE "/api/links/$link_id" "$token_alice")"
expect_status 204 "$status" "DELETE /api/links/{id}"
link_id=""
status="$(request GET "/r/$slug" "")"
expect_status 404 "$status" "GET /r/$slug depois da exclusão"

echo "OK: fluxo de ponta a ponta funcionando"
