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

O fluxo principal está implementado: o BFF autentica as rotas privadas e oferece criação, listagem, exclusão e estatísticas de links; o link-service persiste links no PostgreSQL, resolve slugs com cache Redis e publica cliques no Kafka; o stats-service consome os eventos e oferece totais e contagens diárias. Os detalhes dos endpoints e os limites conhecidos estão em [docs/contratos.md](docs/contratos.md).

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

## Autenticação

O Keycloak importa o realm `encurta` só na primeira subida. Depois de editar `infra/keycloak/encurta-realm.json`, recrie o container: `docker compose up -d --force-recreate --wait keycloak`.

Para obter um token (usuários `alice`/`alice123` e `bob`/`bob123`):

```bash
curl -s -d "grant_type=password&client_id=encurta-postman&username=alice&password=alice123" \
  http://localhost:8080/realms/encurta/protocol/openid-connect/token
```

A coleção `docs/postman/encurta.postman_collection.json` faz isso e guarda o token em variáveis. Rotas em `/api/**` exigem `Authorization: Bearer <access_token>`.

## Rodando um componente

BFF (porta 3000) e link-service (porta 3010), a partir da pasta do componente:

```bash
npm install
npm run dev
npm run lint
npm test
```

stats-service (porta 8000), a partir de `stats-service/`:

```bash
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements-dev.txt
uvicorn app.main:app --reload --port 8000
ruff check app tests
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

## CI/CD e produção

O workflow em `.github/workflows/ci.yml` roda lint e testes em pull requests e pushes para `main`, valida o stack com um smoke test e publica as três imagens no GitHub Container Registry após um push bem-sucedido para `main`. As tags publicadas são `latest` e `sha-<commit>`.

Para usar as imagens publicadas, autentique-se no GHCR e configure o namespace da imagem. Em PowerShell:

```powershell
docker login ghcr.io
$env:IMAGE_NAMESPACE = "ghcr.io/<proprietario>/encurta"
$env:IMAGE_TAG = "latest"
docker compose -f docker-compose.yml -f docker-compose.prod.yml pull
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --wait
```

Troque `<proprietario>` pelo dono do repositório. Para fixar uma versão, use `sha-<commit>` como `IMAGE_TAG`. As variáveis e senhas de infraestrutura continuam vindo do `.env`.
