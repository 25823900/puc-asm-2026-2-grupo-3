# Decisões arquiteturais

Este documento registra a estrutura executável do EncurtadorURL e o motivo de cada tecnologia. O ambiente é didático e local; decisões de disponibilidade, escala e operação de produção foram mantidas simples de propósito.

## Modelo C4

As vistas C4 de contexto e containers ficam centralizadas em [docs/visao-geral.md](./visao-geral.md). As vistas de containers estão separadas por fluxo para facilitar a leitura; este documento concentra justificativas tecnológicas, trade-offs e sequências de interação.

Os bancos `links` e `stats` são databases lógicos separados na mesma instância PostgreSQL. Isso estabelece propriedade de dados e credenciais distintas sem introduzir um cluster distribuído no ambiente de demonstração.

## Justificativas

| Tecnologia ou padrão | Motivo da escolha | Trade-off assumido |
|---|---|---|
| Vue 3 + Vite | Vue atende ao cliente web solicitado; Vite gera arquivos estáticos pequenos e rápidos de desenvolver. | O frontend é uma SPA e precisa obter token OIDC no browser. |
| `keycloak-js` + Authorization Code/PKCE | Usa OIDC padrão para cliente público, sem senha do usuário na aplicação e sem client secret embutido no JavaScript. O token fica em memória. | O client e os redirects do Keycloak precisam corresponder à origem publicada. |
| Nginx não privilegiado | Serve os arquivos estáticos e encaminha `/api`, `/r` e `/health` ao BFF na rede Docker. | Adiciona um proxy simples; em produção deve ser colocado atrás de TLS. |
| BFF (Backend for Frontend) | Mantém uma entrada para o browser, valida JWT, propaga identidade e agrega serviços. Também evita CORS entre a SPA e as APIs. | É uma dependência comum das operações de cliente; o redirect curto continua leve e público. |
| Node.js + Express no BFF e link-service | Reaproveita o stack do repositório e atende bem ao I/O HTTP e CRUD do domínio de links. | Dois deployables Node continuam sendo serviços separados, sem compartilhar código de domínio. |
| Go + net/http no stats-service | Preserva a implementação do novo repositório, com consumo Kafka em lotes e consultas sobre agregados diários. | Há dois runtimes no projeto; Docker e o CI padronizam a execução. A migração legada e o cache versionado estão em [stats-go.md](./stats-go.md). |
| PostgreSQL | Persistência transacional e constraints para UUID, slug único, propriedade e deduplicação de eventos. | Uma instância local não é banco distribuído nem alta disponibilidade. |
| Kafka | Desacopla o redirect da gravação/consulta de métricas; indisponibilidade de analytics não deve impedir abrir um link. | A atualização de estatísticas é eventual e pode aparecer com pequeno atraso. |
| Redis | Acelera resolução de slug e leitura repetida de estatísticas; não é fonte da verdade. O serviço Go tem fallback para PostgreSQL. | TTL de resumo é 30 s. No link-service atual, uma falha de Redis pode impedir a resolução; não se presume fallback nesse serviço. |
| Docker Compose | Sobe frontend, serviços e dependências com versões, rede e health checks declarados. | O Compose local usa uma instância de cada infraestrutura e não pretende demonstrar HA. |
| Targets de teste Docker | Unit tests, lint e build usam Node e Go em containers; o CI também mantém testes nativos e de integração. | Docker é o pré-requisito local; não é preciso instalar Node/Go na máquina. |

## Fluxo de autenticação e API

```mermaid
sequenceDiagram
    actor Pessoa
    participant Vue as Vue + keycloak-js
    participant KC as Keycloak
    participant Nginx as Nginx frontend
    participant BFF as BFF
    participant Link as link-service
    participant DB as PostgreSQL links
    Pessoa->>Vue: Abre localhost:5173
    Vue->>KC: Authorization Code + PKCE (S256)
    KC-->>Vue: Redirect com authorization code
    Vue->>KC: Troca code/verifier por tokens
    KC-->>Vue: Access token
    Vue->>Nginx: POST /api/links + Bearer token
    Nginx->>BFF: Encaminha /api/links
    BFF->>BFF: Valida assinatura, issuer e expiração
    BFF->>Link: POST /links + mesmo Bearer token
    Link->>Link: Valida JWT e obtém sub
    Link->>DB: INSERT link com owner_id = sub
    DB-->>Link: Link criado
    Link-->>BFF: 201 + objeto interno
    BFF-->>Vue: 201 + shortUrl público
```

## Fluxo de resolução e estatísticas

```mermaid
sequenceDiagram
    actor Visitante
    participant Nginx as Nginx frontend
    participant BFF as BFF
    participant Link as link-service
    participant Redis as Redis
    participant Kafka
    participant Stats as stats-service
    participant DB as PostgreSQL stats
    Visitante->>Nginx: GET /r/{slug}
    Nginx->>BFF: Encaminha redirect público
    BFF->>Link: GET /resolve/{slug}
    Link->>Redis: Busca links:slug:{slug}
    alt cache miss
        Link->>Link: Consulta PostgreSQL links
        Link->>Redis: Guarda slug por 3600 s
    end
    Link->>Kafka: Publica LinkClicked(eventId, linkId, clickedAt)
    Link-->>BFF: targetUrl
    BFF-->>Visitante: 302 Location: targetUrl
    Kafka-->>Stats: Entrega evento
    Stats->>DB: INSERT ON CONFLICT(event_id) DO NOTHING
    Note over Stats,DB: Offset confirmado após a gravação
    Vue->>Nginx: GET /api/links/{id}/stats + Bearer
    Nginx->>BFF: Encaminha API
    BFF->>Link: Confirma propriedade do link
    BFF->>Stats: Consulta contagem e série diária
    Stats->>DB: Consulta contadores diários UTC
    Stats-->>BFF: Estatísticas
    BFF-->>Vue: JSON de estatísticas
```

## Limites do ambiente

- Usuários e senhas do realm são fixtures locais; não são credenciais para produção.
- Keycloak inicia em `start-dev`, Kafka usa um broker e PostgreSQL usa uma instância. Esses defaults simplificam apresentação e desenvolvimento, não segurança/alta disponibilidade de produção.
- O frontend usa HTTP local. Em implantação real, configure HTTPS, redirect URIs/domínios apropriados, secrets e política de backup.
- Existe um container `otel-lgtm` com Grafana, Tempo, Prometheus e Loki; BFF, `link-service` e `stats-service` exportam traces, métricas e logs por OTLP. Credenciais do Grafana (`admin`/`admin`) são de demonstração.