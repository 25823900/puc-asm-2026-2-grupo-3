# Orientações do projeto para agentes

## Contexto

O EncurtadorURL é um encurtador de URLs dividido em BFF, `link-service` e `stats-service`. Leia [docs/visao-geral.md](../docs/visao-geral.md) para arquitetura e estado de implementação e [docs/contratos.md](../docs/contratos.md) para os formatos e obrigações de integração.

## Regras para mudanças

- Preserve a independência dos serviços: não importe código de outro serviço; integre por HTTP ou Kafka conforme o contrato.
- Trate `docs/contratos.md` como o acordo entre componentes. Atualize-o junto com qualquer mudança incompatível ou nova integração e considere todos os produtores e consumidores envolvidos.
- Diferencie contrato planejado de funcionalidade implementada. A visão geral registra lacunas conhecidas; confirme código, configuração e testes antes de declarar uma capacidade existente.
- Siga os padrões locais de linguagem, scripts, testes e Dockerfile de cada serviço. Não introduza dependências ou infraestrutura compartilhada sem necessidade comprovada.
- Preserve o formato comum de health check e o formato de erros acordado; não exponha serviços internos ou dados de usuário sem revisar o modelo de acesso.
- Para mudanças de API, persistência, eventos, cache, autenticação ou infraestrutura, consulte também as instruções especializadas em `.github/instructions/contratos-e-integracoes.instructions.md`.
- Execute os testes e linters afetados. Comandos de referência e execução local estão no [README](../README.md) e em [docs/visao-geral.md](../docs/visao-geral.md).
