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

Fase 2: o `docker-compose` sobe toda a infraestrutura e os três componentes, que por enquanto são esqueletos e respondem apenas `GET /health`.

## Subindo o ambiente

Requer Docker com Compose. A partir da raiz do repositório:

```bash
docker compose up -d --build --wait
```

O comando termina quando todos os containers estão saudáveis.

| Serviço | Endereço | Acesso |
|---|---|---|
| BFF | http://localhost:3000 | |
| Keycloak | http://localhost:8080 | usuário `admin`, senha `admin` |
| Interface do Kafka | http://localhost:8081 | |

Não é preciso criar um `.env`. Se alguma porta já estiver em uso na sua máquina, copie o `.env.example` para `.env` e altere o valor.

Para parar e apagar os dados (banco e tópicos):

```bash
docker compose down -v
```

O script `infra/postgres/init.sql` só roda quando o volume do banco é criado. Depois de alterá-lo, é preciso apagar os dados com o comando acima.

### Acesso à infraestrutura pelo host

Para desenvolver um componente fora do Docker, o arquivo `docker-compose.dev.yml` publica no host o PostgreSQL (5432), o Redis (6379), o Kafka (29092), o link-service (3010) e o stats-service (8000):

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build --wait
```

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
