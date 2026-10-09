# Encurta

Encurtador de URLs com estatísticas de cliques. Projeto da disciplina de Arquitetura de Software Multiplataforma.

## Arquitetura

```
Cliente (Postman, web ou app)
        │ HTTP
        ▼
BFF — Node.js (Express)
  ├──HTTP──► link-service — Node.js (Express) — PostgreSQL + Redis
  │                │ publica LinkClicked
  │                ▼
  │              Kafka
  │                │ consome
  │                ▼
  └──HTTP──► stats-service — Python (FastAPI) — PostgreSQL + Redis

Keycloak: autenticação (realm "encurta")
OpenTelemetry + Grafana: logs, métricas e traces
```

Os formatos de API, o evento, as portas e as variáveis de ambiente estão em [docs/contratos.md](docs/contratos.md).

## Estrutura

| Pasta | Conteúdo |
|---|---|
| `bff/` | Ponto de entrada único: autenticação, redirecionamento e agregação |
| `link-service/` | Cadastro de links, cache de slugs e publicação de cliques |
| `stats-service/` | Consumo de cliques e resumo de estatísticas |
| `infra/` | Configuração de PostgreSQL, Kafka e Keycloak |
| `docs/` | Contratos e documentação de arquitetura |

Cada componente tem as próprias dependências e o próprio Dockerfile. Nenhum importa código de outro.

## Estado atual

Fase 1: os três componentes são esqueletos que respondem apenas `GET /health`. O `docker-compose` com a infraestrutura entra na fase 2.

## Rodando um componente

BFF (porta 3000) e link-service (porta 3010), a partir da pasta do componente:

```bash
npm install
npm run dev
npm test
```

stats-service (porta 8000), a partir de `stats-service/`:

```bash
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements-dev.txt
uvicorn app.main:app --reload --port 8000
pytest
```

No Linux e no macOS, a ativação do ambiente é `source .venv/bin/activate`.

## Construindo as imagens

A partir da raiz do repositório:

```bash
docker build -t encurta/bff ./bff
docker build -t encurta/link-service ./link-service
docker build -t encurta/stats-service ./stats-service
```
