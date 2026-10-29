---
description: "Use when adding or changing API endpoints, service-to-service calls, authentication, PostgreSQL, Redis, Kafka events, Docker Compose, environment variables, or observability integrations in EncurtadorURL."
name: "Contratos e integrações do EncurtadorURL"
applyTo:
  - "bff/**"
  - "link-service/**"
  - "stats-service/**"
  - "infra/**"
  - "docker-compose*.yml"
  - "docs/contratos.md"
---

# Contratos e integrações

Antes de editar, leia [docs/contratos.md](../../docs/contratos.md) e [docs/visao-geral.md](../../docs/visao-geral.md). Os contratos documentam parte do comportamento alvo; não presuma que uma rota, integração ou dependência já esteja implementada.

## Evolução de APIs e eventos

- Mantenha JSON em `camelCase`, IDs UUID, datas UTC ISO 8601 e os códigos HTTP e formato de erro definidos no contrato.
- Preserve as responsabilidades: BFF para entrada e agregação; `link-service` para links; `stats-service` para estatísticas.
- Use somente os contratos HTTP e Kafka para comunicação entre serviços. Mantenha cada serviço compilável e testável sem importar módulos de outro serviço.
- Ao alterar um endpoint, atualize seus produtores, consumidores, testes e documentação. Para eventos, considere ordenação por chave, duplicatas e a semântica de entrega pelo menos uma vez.
- Trate indisponibilidade de serviço upstream conforme o contrato e não transforme falhas em respostas de sucesso ou dados inventados.

## Identidade e dados

- A API pública autenticada deve validar emissor, assinatura e expiração conforme o contrato. O `ownerId` deve vir da identidade validada, nunca de um campo controlado pelo cliente.
- Preserve a regra de não revelar se um link privado pertence a outro usuário quando o contrato exige `404`.
- Adicione schema ou migrações de forma compatível com a persistência; `infra/postgres/init.sql` é executado somente na criação do volume e atualmente cria bancos e usuários, não tabelas de domínio.
- Mantenha chaves Redis com prefixo do serviço e respeite invalidação e TTL descritos no contrato. Considere o comportamento correto em cache miss e indisponibilidade.
- Nunca coloque credenciais reais em código, documentação versionada ou fixtures. Use variáveis de ambiente e mantenha `.env.example` sincronizado com as novas configurações.

## Infraestrutura e testes

- Se adicionar uma integração ou serviço, atualize dependências, health checks, `depends_on`, portas e arquivos Compose aplicáveis, Dockerfile, CI e documentação conforme necessário.
- Não exponha um serviço interno no host por padrão; use a configuração de desenvolvimento para publicação local restrita.
- Adicione testes que validem o comportamento e os formatos de fronteira, incluindo falhas de dependências quando relevantes.
- Rode os comandos específicos do serviço (`npm run lint` e `npm test` para BFF/link-service; `npm test` e `npm run build` para frontend; `go vet ./...` e `go test -race -cover ./...` para stats-service). Para mudanças no Compose, valide também o caminho de execução integrado disponível.
- Atualize [docs/contratos.md](../../docs/contratos.md) quando a mudança afetar contratos e [docs/visao-geral.md](../../docs/visao-geral.md) quando arquitetura, responsabilidades ou estado de implementação mudar.
