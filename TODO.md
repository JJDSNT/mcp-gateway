# MCP Gateway Lab

## Objetivo

Gateway MCP para laboratório com foco em:

- **bridge HTTP/SSE ↔ STDIO**
- **endpoint MCP unificado** (`/mcp/<tool>`)
- **orquestração de ferramentas MCP**
- **auth + publicação segura na Internet** (Cloudflare Tunnel + Access)
- **workspace sandbox**
- **Windows + WSL2 + Docker**
- **core implementado em Go**

---

## Estado Atual do Projeto

O projeto encontra-se em **estado funcional de laboratório (MVP)**:

- Bridge HTTP/SSE ⇄ STDIO funcionando
- Native Runtime e Container Runtime operacionais
- Execução via launcher
- Streaming SSE validado
- Integração com Caddy e Cloudflare Tunnel validada

No entanto, **existem pendências técnicas conhecidas** antes de considerar uso prolongado ou exposição pública.

---

## Pendências Técnicas Conhecidas

As pendências abaixo são **conhecidas, mapeadas e intencionais**, alinhadas ao caráter experimental do projeto.

### 1) Gerenciamento de Recursos (Goroutines)

- Goroutines de leitura de `stderr` não estão explicitamente vinculadas ao ciclo de vida do request.
- Em cenários de erro ou cancelamento, podem sobreviver mais tempo que o necessário.

**Mitigação planejada**
- Vincular goroutines ao `context.Context`
- Cancelar leitura ao finalizar request ou matar processo

---

### 2) Tratamento de Erros de I/O

- Erros de `stdin.Close()` são ignorados
- Erros de `scanner.Err()` podem ser perdidos

**Mitigação planejada**
- Log explícito de falhas de fechamento de stdin
- Propagação de erros de leitura do stdout

---

### 3) Timeouts de Execução

- Não há timeout máximo por request ou tool
- Tools travadas podem consumir recursos indefinidamente

**Mitigação planejada**
- Timeout global configurável
- Timeout opcional por tool no `config.yaml`

---

### 4) Validação de Configuração

- `config.yaml` é carregado sem validação semântica
- Erros como `runtime` inválido ou ausência de `cmd/image` só aparecem em runtime

**Mitigação planejada**
- Validação explícita na inicialização:
  - runtime ∈ {native, container}
  - mode ∈ {launcher, daemon}
  - native ⇒ `cmd` obrigatório
  - container ⇒ `image` obrigatório

---

### 5) Escrita Concorrente em SSE

- Em caso de erro após início do streaming, múltiplos writes podem ocorrer
- Atualmente isso não corrompe o stream, mas dificulta semântica de erro

**Mitigação planejada**
- Controle explícito de estado (`sentAny`)
- Erros registrados primariamente via log

---

### 6) Segurança do Container Runtime (docker run)

O Container Runtime utiliza `docker run` com acesso ao Docker socket.

Riscos atuais:
- Privilégio equivalente a root no host
- Containers com:
  - acesso à rede
  - filesystem gravável
  - capabilities padrão

**Mitigação planejada**
- Tornar política de segurança configurável por tool:
  - `network: none|bridge`
  - `read_only: true|false`
  - `cap_drop`
  - `security_opt`
  - `tmpfs`

**Nota**
- Hardening agressivo **não será default**, pois pode quebrar tools legítimas (git, installers, downloads).

---

### 7) Observabilidade e Shutdown

- Logging é não estruturado
- Não há shutdown gracioso do servidor HTTP

**Mitigação planejada**
- Logging estruturado (`log/slog`)
- Captura de SIGTERM/SIGINT
- `http.Server.Shutdown()` com timeout

---

### 8) Rate Limiting

- Não há limitação de taxa por tool ou cliente
- Uso abusivo pode causar exaustão de recursos

**Mitigação planejada**
- Rate limiting simples por tool
- Integração futura com camada de auth (Cloudflare Access)

---

## Segurança e Exposição Pública

Este projeto **não deve ser exposto publicamente** sem:

- Cloudflare Tunnel + Access
- Rate limiting ativo
- Revisão das flags de segurança do Container Runtime
- Isolamento adequado de workspaces

O gateway é **um execution gateway**, não apenas um proxy HTTP.

---

## Filosofia do Projeto

Este projeto prioriza:

- Clareza arquitetural
- Aprendizado profundo de MCP
- Flexibilidade de runtime
- Infraestrutura explorável

Overengineering **não é um problema**, desde que seja consciente e documentado.

---

## Roadmap Técnico (Próximos Passos)

Prioridade alta:
1. Validação de config.yaml
2. Timeouts por tool
3. Correção de leaks e erros silenciosos
4. Hardening configurável do Container Runtime

Prioridade média:
5. Rate limiting
6. Shutdown gracioso
7. Logging estruturado

Prioridade baixa:
8. Pools de daemon
9. Métricas
10. Scheduling inteligente

---

Este README documenta **não apenas o que o sistema faz**, mas **o que ele ainda não faz e por quê**.
Isso é intencional e parte do design do laboratório.
