# Stats-service em Go

O serviço de estatísticas foi migrado para Go 1.26, usando o serviço desenvolvido no projeto encurtador-url-trabalho-completo como base. Mantém porta interna 8000, endpoints, formatos públicos, tópico link.clicked e grupo consumidor stats-service.

## Organização

```text
stats-service/
├── cmd/stats-service/main.go
├── internal/stats/
│   ├── service.go
│   ├── http.go
│   ├── model.go
│   ├── repository.go
│   ├── cache.go
│   ├── consumer.go
│   ├── telemetry.go
│   └── *_test.go
├── Dockerfile
├── go.mod
└── go.sum
```

O entry point inicia o pacote interno. service.go conecta infraestrutura e encerra recursos; http.go atende consultas; model.go valida eventos; repository.go mantém histórico e agregados; consumer.go processa Kafka; cache.go gerencia Redis; telemetry.go configura observabilidade. Os testes ficam junto ao pacote.

## Compatibilidade com o Python deste repositório

- O histórico continua na tabela click_events, com event_id como chave de deduplicação. A tabela antiga, inclusive slugs históricos de até 255 caracteres e sem received_at, é preservada.
- No primeiro startup, uma transação cria link_daily_counts(link_id, day, clicks), agrega todo o histórico em UTC e registra a versão em stats_migrations. Advisory lock e lock de click_events protegem o backfill; reiniciar não repete a contagem.
- Use somente o consumidor Go depois da migração. O consumidor Python não mantém os novos agregados; voltar a Python e depois a Go exigiria reconstrução dos contadores.
- Resumos completos usam stats:summary:v2:{linkId} e versão stats:version:v2:{linkId}, evitando ler o cache parcial deixado pelo Python. TTL do resumo permanece em 30 s.
- Parâmetros inválidos agora retornam 400 VALIDATION_ERROR no formato de contratos.md, substituindo o 422 padrão do FastAPI. O endpoint de totais não usa cache; X-Cache é retornado no resumo por link.
- Não há geração automática de /docs ou /openapi.json. O contrato e a coleção Postman documentam a API.

## Otimizações

Consultas usam os contadores diários, sem varrer todos os cliques. O consumidor acumula até 128 mensagens ou 25 ms; uma instrução SQL grava eventos novos e incrementa os agregados atomicamente. Reentregas não duplicam a contagem. Offsets são confirmados depois da persistência; falhas repetem o lote com backoff.

pgxpool mantém até 16 conexões. Redis tem prazos de 100 ms e retries desativados, com fallback para PostgreSQL. Singleflight compartilha consultas simultâneas do mesmo link. Scripts Lua versionam invalidações para impedir que uma consulta concorrente republique cache obsoleto.

Docker compila um binário estático em múltiplos estágios. A imagem final scratch roda sem privilégios e executa /stats-service healthcheck. SDKs de traces, métricas e logs OTLP são ativados quando OTEL_EXPORTER_OTLP_ENDPOINT está definido; o Compose deste repositório aponta para o container otel-lgtm. O serviço registra encurta_cache_requests_total (cache do resumo) e encurta_stats_clicks_processed_total.

## Executar

```bash
docker compose up -d --build --wait
# Dentro de stats-service:
go run ./cmd/stats-service
go vet ./...
go test -race -cover ./...
```

O projeto Compose chama-se puc-asm-grupo3, separado do encurta original. Neste computador, o .env local define BFF_PORT=3100, KEYCLOAK_PORT=8180 e KAFKA_UI_PORT=8181 para coexistir com o outro ambiente. Em uma máquina sem conflitos, as portas padrão continuam 3000, 8080 e 8081. O .env não é versionado.

## Testes

As suítes verificam contratos HTTP, validação, cache, consultas concorrentes, deduplicação, retry e propagação de trace. Para testes reais, configure TEST_DATABASE_URL, TEST_REDIS_URL e TEST_KAFKA_BROKERS. Sem as variáveis correspondentes, os testes de integração são explicitamente pulados.

O teste de PostgreSQL cria um schema temporário com o formato legado de click_events, verifica migração e reinício, preservação do histórico, atomicidade e incrementos concorrentes. O teste Kafka usa tópico temporário de duas partições e publica 300 eventos em duplicata mais uma mensagem inválida; o total deve ser exatamente 300. Schema, tópico e chaves Redis de teste são removidos ao final.

Smoke de ponta a ponta pelo BFF:

```bash
BFF_URL=http://localhost:3100 KEYCLOAK_URL=http://localhost:8180 bash scripts/smoke-e2e.sh
```

O CI executa gofmt, vet, race detector, build Docker, smoke e integração com PostgreSQL, Redis e Kafka.
