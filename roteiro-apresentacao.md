# Roteiro de apresentação: 20 minutos, 7 integrantes

## Objetivo

Apresentar o problema, a arquitetura C4, as decisões tecnológicas e um fluxo executável de autenticação, criação de link e processamento de estatísticas. Cada integrante tem um bloco e uma passagem clara para a próxima pessoa.

| Integrante | Tempo | Tema |
|---|---:|---|
| 1 | 2 min | Problema, objetivo e cenário de uso |
| 2 | 3 min | C4: contexto, containers e limites dos serviços |
| 3 | 3 min | Frontend e autenticação OIDC/PKCE |
| 4 | 3 min | BFF, API pública e segurança |
| 5 | 3 min | link-service, PostgreSQL e Redis |
| 6 | 3 min | Kafka, stats-service e consistência eventual |
| 7 | 3 min | Docker, testes, trade-offs e encerramento |
| **Total** | **20 min** | |

## Passo a passo

### Integrante 1 - problema e objetivo (00:00-02:00)

**Fala:** “O projeto encurta URLs e permite que cada usuário acompanhe os acessos aos próprios links. O objetivo arquitetural é separar o cliente, a entrada da API, o domínio de links e o processamento de estatísticas sem compartilhar código entre serviços.”

**Mostrar:** tela inicial do Encurta. Apresentar um exemplo de URL longa e o resultado curto.

**Ponto-chave:** distinguir os dois usos: gerenciamento privado autenticado e redirecionamento público.

**Transição:** “A próxima pessoa mostra como essa experiência se transforma nos elementos do modelo C4.”

### Integrante 2 - modelo C4 e limites (02:00-05:00)

**Fala:** “No contexto, a pessoa usa o Encurta e o Keycloak fornece identidade. No nível de containers, o browser passa pelo frontend e pelo BFF; os serviços de links e estatísticas mantêm responsabilidades e dados separados.”

**Mostrar:** as vistas C4 de contexto e containers em [visão geral](./visao-geral.md).

**Explicar:** o Nginx encaminha `/api` e `/r`; o BFF não deixa o browser conhecer endereços internos. `link-service` é dono dos links. `stats-service` é dono dos eventos e agregações.

**Ponto-chave:** PostgreSQL é uma instância com dois databases lógicos, não um banco distribuído. Keycloak é externo à lógica de negócio, embora rode em container no ambiente local.

**Transição:** “Com os limites definidos, vamos seguir o primeiro contato do usuário: a autenticação no frontend.”

### Integrante 3 - frontend e autenticação (05:00-08:00)

**Fala:** “O cliente foi feito em Vue 3 e compilado pelo Vite. O `keycloak-js` usa Authorization Code com PKCE: a aplicação não recebe a senha nem embute um segredo de client no JavaScript. O token fica em memória.”

**Demonstrar:** clicar em **Entrar com Encurta**, autenticar como `alice` (`alice123`) e mostrar o retorno ao painel. As credenciais são fixtures de demonstração local.

**Explicar:** Nginx serve os arquivos gerados e faz o proxy reverso para o BFF. O browser chama uma única origem, o que simplifica as chamadas e evita CORS entre frontend e APIs.

**Motivo das escolhas:** Vue atende ao cliente web solicitado e organiza a interface em componentes; Vite oferece build estático; Keycloak centraliza identidade e permite usar um padrão OIDC apropriado para SPA.

**Transição:** “Autenticado, o browser não chama o serviço de links diretamente; ele passa pelo BFF.”

### Integrante 4 - BFF e segurança da API (08:00-11:00)

**Fala:** “O BFF é a entrada da API pública. Ele valida assinatura, emissor e expiração do JWT, encaminha o mesmo bearer token ao `link-service` e agrega dados do `stats-service`.”

**Mostrar:** no painel, criar uma URL e atualizar a lista. Se for útil, mostrar no Postman uma chamada sem token retornando `401` e a mesma chamada autenticada.

**Explicar:** o BFF confirma a propriedade do link antes de consultar estatísticas individuais. A listagem permanece disponível se o serviço de estatísticas falhar; nesse caso, `totalClicks` pode vir como `null`.

**Motivo das escolhas:** Express mantém o padrão Node existente; o BFF evita expor serviços internos e centraliza adaptação das respostas para o frontend.

**Transição:** “O BFF encaminha o pedido; agora veremos onde a regra de propriedade e a persistência do link realmente vivem.”

### Integrante 5 - links, PostgreSQL e Redis (11:00-14:00)

**Fala:** “O `link-service` valida o token novamente e obtém o proprietário da claim `sub`; o cliente não pode escolher o `ownerId`. Links são gravados no database `links`, com UUID, slug único de sete caracteres, URL e data.”

**Mostrar:** relacionar o link recém-criado na interface com o registro persistido e abrir seu endereço curto. O destino responde por redirecionamento `302`.

**Explicar:** PostgreSQL é a fonte de verdade. Redis acelera a resolução por slug e a exclusão invalida a chave. Se Redis falhar, a resolução pode continuar pelo banco.

**Motivo das escolhas:** PostgreSQL fornece transações e constraints para integridade; Redis é apenas cache, não armazenamento definitivo. Manter esses papéis separados evita perder dados quando o cache expira.

**Transição:** “A resolução também inicia o caminho assíncrono que transforma cliques em estatísticas.”

### Integrante 6 - Kafka e estatísticas (14:00-17:00)

**Fala:** “Após uma resolução válida, o `link-service` publica `LinkClicked` no Kafka. O `stats-service`, escrito em Go com net/http, consome o evento e grava no database `stats`.”

**Mostrar:** atualizar o painel ou abrir as estatísticas do link após o clique. Se o evento ainda estiver sendo processado, explicar o pequeno atraso e atualizar novamente.

**Explicar:** Kafka desacopla o redirect da gravação de analytics; uma falha de publicação não deve impedir o redirecionamento. A entrega é pelo menos uma vez e `event_id` único evita contagem duplicada. Redis mantém o resumo por 30 segundos e é invalidado quando chega um evento novo.

**Motivo das escolhas:** net/http fornece os endpoints de consulta e Go permite processamento concorrente; Kafka deixa a análise fora do caminho crítico do redirect; banco separado estabelece propriedade do dado de analytics.

**Transição:** “O fluxo funcional está completo; a última parte mostra como reproduzimos o ambiente e o que não estamos afirmando sobre produção.”

### Integrante 7 - execução, testes e trade-offs (17:00-20:00)

**Fala:** “Docker Compose inicia frontend, serviços e dependências com health checks e migrações. Os testes usam containers com Node e Go fixados, então não exigem runtimes instalados na máquina.”

**Mostrar:** `docker compose ps`, destacar frontend, BFF, serviços, PostgreSQL, Redis, Kafka e Keycloak. Mostrar os targets `test-frontend`, `test-bff`, `test-link-service` e `test-stats-service` no Compose de testes.

**Motivo das escolhas:** Compose reduz diferenças entre máquinas; targets de teste e GitHub Actions reproduzem as verificações em containers. O Nginx é não privilegiado no runtime do frontend.

**Limites a declarar:** o ambiente usa credenciais de demonstração, Keycloak em `start-dev`, um broker e uma instância PostgreSQL. Não demonstra alta disponibilidade, TLS ou topologia distribuída de banco. A observabilidade (otel-lgtm) é de demonstração, sem alertas.

**Encerramento:** “A separação permite que o redirect continue leve e que a análise evolua de forma independente. O preço é a consistência eventual das estatísticas e mais containers para operar.”

## Preparação da demonstração

1. Na raiz do projeto, executar `docker compose up -d --build --wait` e confirmar os serviços com `docker compose ps`.
2. Abrir `http://localhost:5173`; entrar com `alice` / `alice123` ou `bob` / `bob123`.
3. Criar uma URL de demonstração, por exemplo `https://example.com/apresentacao`.
4. Abrir o link curto em uma nova aba; ele deve responder com `302` para o destino.
5. Voltar ao painel e abrir **Estatísticas**. O clique é assíncrono e pode levar alguns instantes para aparecer.
6. Manter o painel já autenticado e um link preparado como contingência caso a autenticação ou o consumo Kafka demore durante a apresentação.

Não mostrar access tokens, senha do administrador ou dados pessoais. As contas listadas são apenas usuários de teste locais.

## Justificativas resumidas das tecnologias

| Tecnologia | Responsabilidade | Por que foi escolhida | Limite consciente |
|---|---|---|---|
| Vue 3 + Vite | Interface e build da SPA | Componentização simples e geração de arquivos estáticos; Vue foi a tecnologia de frontend solicitada. | A SPA precisa de um fluxo OIDC para obter identidade no browser. |
| `keycloak-js` + PKCE | Login do frontend | Padrão OIDC para cliente público; evita senha e client secret no código do browser. | Redirect URIs devem corresponder à origem e à porta configuradas. |
| Nginx | Servir a SPA e encaminhar rotas | Runtime estático leve, uma origem para `/api` e `/r`, sem expor serviços internos ao browser. | Requer TLS e configuração de domínio em implantação real. |
| Express | BFF e domínio de links | Reaproveita o stack Node do repositório e simplifica APIs HTTP orientadas a I/O. | BFF e `link-service` continuam deployables diferentes e devem manter contratos. |
| Go / net/http | Consulta e consumo de estatísticas | Mantém analytics isolado, processa eventos em lotes e consulta agregados diários. | Binário estático em Docker; histórico preservado em click_events. |
| PostgreSQL | Persistência durável | Transações e constraints garantem slug único, proprietário e deduplicação. | Uma instância com databases lógicos distintos não é banco distribuído nem HA. |
| Kafka | Transporte assíncrono dos cliques | Remove gravação de analytics do caminho crítico de redirecionamento. | Estatísticas são eventualmente consistentes e podem atrasar alguns segundos. |
| Redis | Cache de resolução/resumo | Reduz leituras repetidas sem substituir a fonte de verdade. | Cache precisa de TTL e invalidação; pode ficar desatualizado até o próximo processamento. |
| Docker Compose | Ambiente integrado e testes | Declara imagens, rede, dependências, health checks e migrações em um fluxo reproduzível. | Topologia local simples; não representa escalabilidade ou alta disponibilidade. |
| GitHub Actions | Verificação e publicação | Automatiza testes containerizados, smoke test e imagens em pull requests/pushes. | O workflow verifica este stack de demonstração; não substitui testes de carga/produção. |

Para diagramas, contratos HTTP/Kafka e detalhes técnicos complementares, consulte [visão geral](./visao-geral.md), [contratos](./contratos.md) e [decisões arquiteturais](./decisoes-arquiteturais.md).