# Encurta

Encurtador de URLs com estatísticas de cliques. Projeto da disciplina de Arquitetura de Software Multiplataforma.

## Arquitetura

```
Navegador (Vue 3 + Keycloak OIDC/PKCE)
        │ HTTP, mesma origem
        ▼
Frontend — Nginx (SPA e proxy /api, /r e /health)
        │ HTTP
        ▼
BFF — Node.js (Express)
  ├──HTTP──► link-service — Node.js (Express) — PostgreSQL + Redis
  │                │ publica LinkClicked
  │                ▼
  │              Kafka
  │                │ consome
  │                ▼
  └──HTTP──► stats-service — Go (net/http) — PostgreSQL + Redis

Keycloak: autenticação (realm "encurta")
OpenTelemetry: BFF, link-service e stats-service enviam traces, métricas e logs ao otel-lgtm (Grafana, Tempo, Prometheus, Loki)
```

A observabilidade roda no container `otel-lgtm` (Grafana, Tempo, Prometheus, Loki e coletor OTLP), iniciado junto com o restante do Compose.

Os formatos de API, o evento, as portas e as variáveis de ambiente estão em [docs/contratos.md](docs/contratos.md).
Diagramas C4, fluxos e justificativas estão na [visão geral](docs/visao-geral.md) e nas [decisões arquiteturais](docs/decisoes-arquiteturais.md). Veja também o [roteiro de apresentação](docs/roteiro-apresentacao.md).

## Estrutura

| Pasta | Conteúdo |
|---|---|
| `frontend/` | SPA Vue, login OIDC/PKCE e proxy Nginx para o BFF |
| `bff/` | Ponto de entrada único: autenticação, redirecionamento e agregação |
| `link-service/` | Cadastro de links, cache de slugs e publicação de cliques |
| `stats-service/` | Consumo de cliques e resumo de estatísticas |
| `infra/` | Configuração de PostgreSQL, Kafka e Keycloak |
| `docs/` | Contratos e documentação de arquitetura |

Cada componente tem as próprias dependências e o próprio Dockerfile. Nenhum importa código de outro.

## Estado atual

O fluxo principal está implementado: o frontend autentica com OIDC/PKCE e chama o BFF pela mesma origem; o BFF autentica as rotas privadas e oferece criação, listagem, exclusão e estatísticas de links; o link-service persiste links no PostgreSQL, resolve slugs com cache Redis e publica cliques no Kafka; o stats-service em Go consome os eventos e oferece totais e contagens diárias. As implementações de BFF, links e estatísticas do novo repositório foram preservadas na integração do frontend. Os detalhes dos endpoints e os limites conhecidos estão em [docs/contratos.md](docs/contratos.md).

## Subindo o ambiente

Requer Docker com Compose. A partir da raiz do repositório:

```bash
docker compose up -d --build --wait
```

O comando termina quando todos os containers estão saudáveis.

| Serviço | Endereço | Acesso |
|---|---|---|
| Frontend | http://localhost:5173 | `alice`/`alice123` ou `bob`/`bob123` |
| BFF | http://localhost:3000 | |
| Keycloak | http://localhost:8080 | usuário `admin`, senha `admin` |
| Interface do Kafka | http://localhost:8081 | |

Não é preciso criar um `.env`. Se alguma porta já estiver em uso na sua máquina, copie o `.env.example` para `.env` e altere o valor.
Se alterar `FRONTEND_PORT`, ajuste `redirectUris` e `webOrigins` do client `encurta-frontend` em `infra/keycloak/encurta-realm.json` e recrie o Keycloak. `PUBLIC_BASE_URL` aponta para a origem do frontend para que links curtos passem pelo Nginx.

O nome do projeto Compose é `puc-asm-grupo3`, separado do ambiente `encurta`. Neste computador, o `.env` local usa API em http://localhost:3100, Keycloak em http://localhost:8180 e Kafka UI em http://localhost:8181 para permitir executar os dois projetos simultaneamente.

### Observabilidade com OpenTelemetry e Grafana

O container `otel-lgtm` sobe com o Compose padrão e recebe traces, métricas e logs por OTLP/HTTP (`http://otel-lgtm:4318`) do BFF, do `link-service` e do `stats-service`. Abra http://localhost:3001 e entre com `admin`/`admin` por padrão (ou configure `GRAFANA_ADMIN_PASSWORD` no `.env`). A porta pode ser alterada com `GRAFANA_PORT`.

- **Dashboard:** em **Dashboards**, abra **EncurtadorURL**. Mostra cliques publicados e gravados, falhas de publicação, taxa de acerto e volume do cache (Redis do `link-service` e do `stats-service`) e os logs dos três serviços.
- **Logs:** os serviços escrevem JSON no stdout com `trace_id` e `span_id`. No **Explore**, fonte **Loki**, consulte `{service_name="bff"}`, `{service_name="link-service"}` ou `{service_name="stats-service"}`.
- **Traces:** no **Explore**, fonte **Tempo**, busque por `service.name`. Um `trace_id` visto em um log leva ao trace distribuído BFF → link-service → Kafka → stats-service.
- **Métricas:** `encurta_cache_requests_total` (rótulos `cache` e `result`), `encurta_link_clicks_total` (rótulo `outcome`: `published` ou `failed`) e `encurta_stats_clicks_processed_total`, além das métricas HTTP e de runtime dos SDKs.

Dashboard e provisionamento ficam em `infra/observability/`. Os dados ficam no volume `lgtm-data` e são removidos com `docker compose down -v`. Grafana é publicado apenas em `127.0.0.1`; as portas OTLP não são publicadas no host. Sem `OTEL_EXPORTER_OTLP_ENDPOINT`, os serviços funcionam normalmente e não exportam telemetria.

Para parar e apagar os dados (banco e tópicos):

```bash
docker compose down -v
```

O script `infra/postgres/init.sql` só cria bancos e usuários quando o volume é criado. As tabelas são criadas idempotentemente pelos próprios serviços na inicialização; o serviço Go também migra o histórico de cliques sem apagar volumes. A integração não usa as migrações SQL do serviço Python antigo.

### Acesso à infraestrutura pelo host

Para desenvolver um componente fora do Docker, o arquivo `docker-compose.dev.yml` publica no host o PostgreSQL (5432), o Redis (6379), o Kafka (29092), o link-service (3010) e o stats-service (8000):

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build --wait
```

## Autenticação

O frontend usa `keycloak-js` com Authorization Code/PKCE (`S256`) e mantém o token em memória. O client público `encurta-frontend` não recebe senhas nem possui segredo no navegador. O client `encurta-postman` mantém o fluxo de senha exclusivamente para testes locais.

O Keycloak importa o realm `encurta` só na primeira subida. Depois de editar `infra/keycloak/encurta-realm.json`, recrie o container: `docker compose up -d --force-recreate --wait keycloak`.

Para obter um token (usuários `alice`/`alice123` e `bob`/`bob123`):

```bash
curl -s -d "grant_type=password&client_id=encurta-postman&username=alice&password=alice123" \
  http://localhost:8080/realms/encurta/protocol/openid-connect/token
```

A coleção `docs/postman/encurta.postman_collection.json` faz isso e guarda o token em variáveis. Rotas em `/api/**` exigem `Authorization: Bearer <access_token>`.

## Rodando um componente

Frontend, a partir de `frontend/`:

```bash
npm ci
npm run dev
npm test
npm run build
```

O comando `dev` inicia apenas o Vite, sem o proxy de API do Nginx. Use o Compose para validar o fluxo integrado. Para Keycloak fora da porta padrão, use `VITE_KEYCLOAK_URL`. O frontend requer Node 24 ou superior.

BFF (porta 3000) e link-service (porta 3010), a partir da pasta do componente:

```bash
npm install
npm run dev
npm run lint
npm test
```

stats-service (porta 8000), a partir de `stats-service/`:

```bash
go run ./cmd/stats-service
go vet ./...
go test -race -cover ./...
```

Requer Go 1.26. A migração conserva a tabela `click_events` do Python, cria agregados diários e não exige apagar volumes. Estrutura, testes e detalhes em [docs/stats-go.md](docs/stats-go.md).

## Testes em containers

Sem instalar Node ou Go na máquina:

```bash
docker compose -f docker-compose.test.yml build
docker compose -f docker-compose.test.yml run --rm test-frontend
docker compose -f docker-compose.test.yml run --rm test-bff
docker compose -f docker-compose.test.yml run --rm test-link-service
docker compose -f docker-compose.test.yml run --rm test-stats-service
```

O target Go executa gofmt, vet e testes com race detector. Testes de integração exigem as variáveis descritas em [docs/stats-go.md](docs/stats-go.md). O CI mantém testes de integração PostgreSQL/Redis/Kafka e agora executa o smoke test pela origem do frontend.

## Construindo as imagens

A partir da raiz do repositório:

```bash
docker build -t encurta/bff ./bff
docker build -t encurta/link-service ./link-service
docker build -t encurta/stats-service ./stats-service
docker build -t encurta/frontend ./frontend
```

## CI/CD e produção

O workflow em `.github/workflows/ci.yml` roda lint, testes e build do frontend em pull requests e pushes para `main`, valida o stack com um smoke test através do Nginx e publica as quatro imagens no GitHub Container Registry após um push bem-sucedido para `main`. As tags publicadas são `latest` e `sha-<commit>`.

Para usar as imagens publicadas, autentique-se no GHCR e configure o namespace da imagem. Em PowerShell:

```powershell
docker login ghcr.io
$env:IMAGE_NAMESPACE = "ghcr.io/<proprietario>/encurta"
$env:IMAGE_TAG = "latest"
docker compose -f docker-compose.yml -f docker-compose.prod.yml pull
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --wait
```

Troque `<proprietario>` pelo dono do repositório. Para fixar uma versão, use `sha-<commit>` como `IMAGE_TAG`. As variáveis e senhas de infraestrutura continuam vindo do `.env`.
