# Contratos

Acordos entre as frentes do projeto. Outras pessoas dependem destes formatos, então qualquer mudança aqui é feita por merge request e avisada ao grupo.

## Estado da implementação

As rotas descritas neste documento estão implementadas: o BFF fornece redirecionamento, gestão autenticada de links e acesso às estatísticas; o link-service fornece persistência, isolamento por proprietário, cache e publicação de cliques; o stats-service consome cliques de Kafka e mantém as consultas agregadas. `GET /health` também está disponível nos três serviços.

## Componentes e portas

| Componente | Nome no `docker-compose` | Porta interna | Porta no host |
|---|---|---|---|
| BFF | `bff` | 3000 | 3000 |
| link-service | `link-service` | 3010 | não publicada |
| stats-service | `stats-service` | 8000 | não publicada |
| PostgreSQL | `postgres` | 5432 | não publicada |
| Redis | `redis` | 6379 | não publicada |
| Kafka | `kafka` | 9092 | não publicada |
| Interface do Kafka | `kafka-ui` | 8080 | 8081 |
| Keycloak | `keycloak` | 8080 | 8080 |
| Grafana e coletor OTLP | `otel-lgtm` | 3000 (Grafana), 4318 (OTLP HTTP) | 3001 (Grafana) |

As portas do BFF, do Keycloak e da interface do Kafka no host podem ser trocadas pelo arquivo `.env` (ver `.env.example`).

Com o `docker-compose.dev.yml`, as portas "não publicadas" ficam acessíveis no host, só em `127.0.0.1`, com o mesmo número. A exceção é o Kafka, que do host atende em `localhost:29092`.

## Convenções gerais

- JSON em `camelCase`.
- Datas em ISO 8601, sempre em UTC (ex.: `2026-10-08T21:30:00.000Z`).
- Identificadores em UUID.
- Slug com 7 caracteres de `0-9a-zA-Z`.
- Todo componente expõe `GET /health` sem autenticação, que responde `200` com `{ "status": "ok", "service": "<nome>" }`.

### Formato de erro

```json
{ "error": { "code": "LINK_NOT_FOUND", "message": "Link não encontrado" } }
```

| Código | Status | Quando |
|---|---|---|
| `VALIDATION_ERROR` | 400 | Corpo ou parâmetro inválido |
| `UNAUTHORIZED` | 401 | Token ausente, inválido ou expirado |
| `LINK_NOT_FOUND` | 404 | Link inexistente ou de outro usuário |
| `UPSTREAM_UNAVAILABLE` | 503 | O BFF não conseguiu falar com um serviço obrigatório |
| `INTERNAL_ERROR` | 500 | Falha não prevista |

## Autenticação

- Realm do Keycloak: `encurta`, importado de `infra/keycloak/encurta-realm.json`. Client público `encurta-postman` (login por usuário e senha) e usuários de teste `alice` (`alice123`) e `bob` (`bob123`), com `sub` fixos `f3c1d2e4-0000-4000-8000-000000000001` e `...0002`. A coleção do Postman está em `docs/postman/`.
- No BFF, as rotas `/api/**` exigem `Authorization: Bearer <JWT>`. As rotas `/r/{slug}` e `/health` são públicas.
- O BFF valida assinatura (JWKS), emissor e expiração, e repassa o mesmo header `Authorization` ao `link-service`.
- O `link-service` valida o token da mesma forma e usa a claim `sub` como dono do link (`ownerId`). Um link de outro usuário é tratado como inexistente (`404`).
- O `stats-service` não recebe token: não é publicado no host, e o BFF só consulta links que o `link-service` já confirmou pertencerem ao usuário.

## API pública (BFF)

Base: `http://localhost:3000`

| Método e rota | Autenticação | Resposta |
|---|---|---|
| `GET /r/{slug}` | Não | `302` com `Location: <targetUrl>`; `404` se o slug não existe |
| `POST /api/links` | Sim | `201` com o link criado |
| `GET /api/links` | Sim | `200` com a lista de links do usuário e o total de cliques |
| `GET /api/links/{id}/stats` | Sim | `200` com os cliques por dia |
| `DELETE /api/links/{id}` | Sim | `204` |

Corpo de `POST /api/links`:

```json
{ "targetUrl": "https://exemplo.com/uma/pagina/longa" }
```

Item de `GET /api/links` (o `POST` devolve o mesmo objeto, sem `totalClicks`):

```json
{
  "id": "7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11",
  "slug": "aB3xK9q",
  "shortUrl": "http://localhost:3000/r/aB3xK9q",
  "targetUrl": "https://exemplo.com/uma/pagina/longa",
  "createdAt": "2026-10-08T21:30:00.000Z",
  "totalClicks": 42
}
```

Se o `stats-service` estiver fora do ar, `GET /api/links` continua respondendo `200`, com `totalClicks: null`. Já `GET /api/links/{id}/stats` responde `503`.

Resposta de `GET /api/links/{id}/stats`: a mesma do `stats-service` (ver abaixo).

## API interna: link-service

Base: `http://link-service:3010`

| Método e rota | Autenticação | Resposta |
|---|---|---|
| `POST /links` | Sim | `201` com o link; `400` se `targetUrl` for inválida |
| `GET /links` | Sim | `200` com os links do usuário do token |
| `GET /links/{id}` | Sim | `200` com o link; `404` |
| `DELETE /links/{id}` | Sim | `204`; `404` |
| `GET /resolve/{slug}` | Não | `200` com o destino; `404` |

Corpo de `POST /links`: `{ "targetUrl": "..." }`. A URL deve ser absoluta, com esquema `http` ou `https`, e ter até 2048 caracteres.

Objeto link:

```json
{
  "id": "7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11",
  "slug": "aB3xK9q",
  "targetUrl": "https://exemplo.com/uma/pagina/longa",
  "ownerId": "f3c1d2e4-0000-4000-8000-000000000001",
  "createdAt": "2026-10-08T21:30:00.000Z"
}
```

Resposta de `GET /resolve/{slug}`:

```json
{ "id": "7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11", "slug": "aB3xK9q", "targetUrl": "https://exemplo.com/uma/pagina/longa" }
```

`GET /resolve/{slug}` devolve o header `X-Cache` e publica o evento `LinkClicked` a cada resolução bem-sucedida, venha ela do cache ou do banco.

## API interna: stats-service

Base: `http://stats-service:8000`

| Método e rota | Resposta |
|---|---|
| `GET /stats?linkIds={id1},{id2}` | `200` com o total de cliques de cada link pedido |
| `GET /stats/{linkId}` | `200` com o total e os cliques por dia |

Resposta de `GET /stats?linkIds=...`:

```json
[
  { "linkId": "7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11", "totalClicks": 42 }
]
```

Resposta de `GET /stats/{linkId}`, que devolve o header `X-Cache`:

```json
{
  "linkId": "7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11",
  "totalClicks": 42,
  "clicksPerDay": [
    { "date": "2026-10-07", "clicks": 30 },
    { "date": "2026-10-08", "clicks": 12 }
  ]
}
```

O `stats-service` não sabe quais links existem. Um link sem cliques, ou desconhecido, responde `200` com `totalClicks: 0` e, na rota por link, `clicksPerDay: []`.

## Evento LinkClicked (Kafka)

| Item | Valor |
|---|---|
| Tópico | `link.clicked` |
| Chave da mensagem | `linkId` |
| Valor | JSON em UTF-8 |
| Produtor | `link-service` |
| Consumidor | `stats-service`, no grupo `stats-service` |
| Partições | 1 no ambiente local |

```json
{
  "eventId": "0d9a6c1e-8f3b-4f0a-a1c2-5e6d7f8091ab",
  "linkId": "7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11",
  "slug": "aB3xK9q",
  "clickedAt": "2026-10-08T21:30:00.000Z"
}
```

- O tópico é criado pela infraestrutura na subida do ambiente, não pelos serviços.
- Falha ao publicar não derruba o redirecionamento: o `link-service` registra o erro e responde normalmente.
- A entrega é "pelo menos uma vez". O consumidor confirma o offset só depois de gravar o clique, então uma mensagem pode chegar repetida. O `eventId` existe para quem quiser descartar duplicatas.
- Os headers da mensagem ficam reservados para a propagação de trace do OpenTelemetry (`traceparent`).

## Cache (Redis)

Uma única instância, com prefixo de chave por serviço.

| Serviço | Chave | Conteúdo | TTL |
|---|---|---|---|
| link-service | `links:slug:{slug}` | Destino do slug | 3600 s |
| stats-service | `stats:summary:{linkId}` | Resposta de `GET /stats/{linkId}` | 30 s |

- O `link-service` apaga a chave do slug ao excluir o link.
- As rotas com cache devolvem `X-Cache: HIT` ou `X-Cache: MISS`.

## Variáveis de ambiente

No `docker-compose.yml`, todas já estão definidas com os valores abaixo. Só portas do host e senhas vêm do `.env`.

Comuns aos três componentes:

| Variável | Exemplo |
|---|---|
| `PORT` | `3000` |
| `OTEL_SERVICE_NAME` | `bff` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://otel-lgtm:4318` |

BFF:

| Variável | Exemplo |
|---|---|
| `PUBLIC_BASE_URL` | `http://localhost:3000` |
| `LINK_SERVICE_URL` | `http://link-service:3010` |
| `STATS_SERVICE_URL` | `http://stats-service:8000` |
| `KEYCLOAK_ISSUER` | `http://localhost:8080/realms/encurta` |
| `KEYCLOAK_JWKS_URL` | `http://keycloak:8080/realms/encurta/protocol/openid-connect/certs` |

link-service:

| Variável | Exemplo |
|---|---|
| `DATABASE_URL` | `postgres://links_user:links_pass@postgres:5432/links` |
| `REDIS_URL` | `redis://redis:6379` |
| `KAFKA_BROKERS` | `kafka:9092` |
| `KAFKA_TOPIC_LINK_CLICKED` | `link.clicked` |
| `KEYCLOAK_ISSUER` | igual ao do BFF |
| `KEYCLOAK_JWKS_URL` | igual ao do BFF |

stats-service:

| Variável | Exemplo |
|---|---|
| `DATABASE_URL` | `postgresql://stats_user:stats_pass@postgres:5432/stats` |
| `REDIS_URL` | `redis://redis:6379` |
| `KAFKA_BROKERS` | `kafka:9092` |
| `KAFKA_TOPIC_LINK_CLICKED` | `link.clicked` |
| `KAFKA_GROUP_ID` | `stats-service` |

`KEYCLOAK_ISSUER` e `KEYCLOAK_JWKS_URL` são separadas de propósito. O emissor que vem dentro do token é o endereço público do Keycloak (`localhost`), mas dentro da rede do Docker as chaves são buscadas pelo nome do container (`keycloak`).
