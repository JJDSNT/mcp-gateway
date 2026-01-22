# Status de Testes - MCP Gateway

## ✅ P0 — Bloqueia incidentes sérios (COMPLETO)

### Workspace sandbox: bloquear path traversal (inclui encoding)
- ✅ `../, %2e%2e%2f, %252e%252e%252f, ..\\, //, /.` bloqueados
- ✅ Rejeita antes de chamar qualquer tool
- ✅ Teste: `TestValidatePath_PathTraversal`
- ✅ Arquivo: `internal/sandbox/sandbox_test.go`

### Workspace sandbox: bloquear "symlink escape"
- ✅ Symlinks absolutos bloqueados: `link → /etc/passwd`
- ✅ Symlinks relativos bloqueados: `link → ../../outside`
- ✅ Cadeias de symlinks bloqueadas: `link1 → link2 → ../outside`
- ✅ Realpath resolvido e validado
- ✅ Testes: `TestValidatePath_SymlinkEscape`, `TestValidatePath_RejectsSymlinkChainEscape`
- ✅ Arquivo: `internal/sandbox/sandbox_test.go`

### Allowlist rígida de tools + validação do <tool> na rota
- ✅ `/mcp/<tool>` só aceita tools no YAML
- ✅ `<tool>` com `/, .., %2f, \, espaços` → 400/404
- ✅ Testes: `TestValidatePath_*`, `TestValidateToolName_*`
- ✅ Arquivo: `internal/sandbox/sandbox_test.go`, `integration_test.go`

### Garantir que execução é "sem shell" (anti command injection)
- ✅ `cmd/args` viram `exec.Command` (sem `sh -c`)
- ✅ `;, |, &&, ||, $(, `, >, <, >>` são argumentos, não comandos
- ✅ 40+ testes de injeção de comando
- ✅ Teste: `TestCommand_*`
- ✅ Arquivo: `internal/sandbox/command_injection_test.go`

### DoS básico: limites e timeouts
- ✅ Body: máximo 1MB
- ✅ Tool travada: timeout/cancelamento (30s default)
- ✅ SSE: máximo 4MB por linha, context timeout
- ✅ Testes: `TestSSEStreamingMemory`, `TestSSEStreamingTimeout`
- ✅ Arquivo: `internal/sandbox/dos_test.go`

**Resultado P0: 80+ testes PASSANDO ✅**

---

## ✅ P1 — Segurança "porque vai pra internet" (COMPLETO)

### Hardening de HTTP: métodos e headers
- ✅ Apenas GET/POST permitidos
- ✅ PUT/DELETE/PATCH/TRACE → 405 Method Not Allowed
- ✅ Content-Type: JSON obrigatório em POST
- ✅ Outros Content-Types → 415 Unsupported Media Type
- ✅ Testes: `TestHTTPMethodNotAllowed`, `TestContentTypeValidation`
- ✅ Arquivo: `internal/sandbox/http_hardening_test.go`, `http_hardening_test.go` (main)

### SSE correctness + anti-cache
- ✅ Content-Type: `text/event-stream`
- ✅ Cache-Control: `no-cache` (obrigatório)
- ✅ Connection: `keep-alive`
- ✅ X-Accel-Buffering: `no` (Nginx/Caddy)
- ✅ Flusher interface (streaming real)
- ✅ Testes: `TestSSEHeadersPresent`, `TestSSENoCache`, `TestSSEFlusherInterface`
- ✅ Arquivo: `internal/sandbox/sse_headers_and_flush_test.go`

### Cancelar tool quando cliente desconecta
- ✅ Cliente dropa túnel → processo encerrado (evita leak/DoS)
- ✅ Context cancelado na desconexão TCP
- ✅ Process kill: SIGTERM → SIGKILL (via `KillProcess()`)
- ✅ Sem goroutine presa
- ✅ Testes: `TestSSEDisconnectKillsProcessContext`, `TestSSEDisconnectDuringStreaming`
- ✅ Arquivo: `internal/sandbox/sse_disconnect_kills_tool_test.go`, `sse_disconnect_kills_tool_test.go` (main)

### Não confiar em headers de "auth" internos
- ✅ Headers `X-Auth`, `Authorization`, `X-Forwarded-*` não mudam validação
- ✅ Headers `CF-Access-Authenticated`, `CF-Ray` não causam bypass
- ✅ Mesmo com Cloudflare Access, sem atalhos acidentais
- ✅ Testes-regressão: falha se alguém introduzir shortcut
- ✅ Testes: `TestAuthHeadersBypassRegression`, `TestAuthHeadersDoNotAffectResponse`
- ✅ Arquivo: `internal/sandbox/auth_header_regression_test.go`

**Resultado P1: 30+ testes PASSANDO ✅**

---

## 📋 P2 — Container runtime (Alto risco, recomendado para depois)

❌ **NÃO INICIADO**

### Testes de "container run" sem privilégios inesperados
- Não permitir flags `--privileged`, `--pid=host`, `--net=host`
- Garantir volumes mínimos (workspace) e read-only se possível
- Arquivo sugerido: `internal/runtime/docker_hardening_test.go`

### Garantir que o gateway não vaza segredos para containers
- Env vars sensíveis não propagadas por padrão
- Arquivo sugerido: `internal/runtime/docker_secrets_test.go`

### Teste específico do alerta docker.sock
- Falhar se alguém facilitar mounts/flags extras
- Arquivo sugerido: `internal/runtime/docker_sock_test.go`

---

## 📝 P3 — Qualidade e observabilidade (Bom ter)

❌ **NÃO INICIADO**

### Testes de concorrência (race)
- Vários clientes na mesma tool (daemon mode)
- Sem corromper stream/mutex
- Arquivo sugerido: `internal/runner/concurrency_test.go`

### Testes de "daemon mode idle timeout"
- Sobe tool → fica ociosa → encerra
- Volta request → sobe novamente
- Arquivo sugerido: `internal/runner/daemon_timeout_test.go`

### Logs/telemetria mínima
- Erros relevantes em stderr/headers MCP
- Sem vazar payload sensível
- Arquivo sugerido: `internal/runner/logging_test.go`

---

## 📊 Resumo de Status

| Priority | Status | Testes | Localização |
|----------|--------|--------|------------|
| **P0** | ✅ COMPLETO | 80+ | `internal/sandbox/` |
| **P1** | ✅ COMPLETO | 30+ | `internal/sandbox/`, `main` |
| **P2** | ❌ Não iniciado | 0 | (proposto) |
| **P3** | ❌ Não iniciado | 0 | (proposto) |

**Total: ~110 testes de segurança implementados e passando ✅**

---

## Como Rodar os Testes

**P0 + P1 (Security Critical & Hardening):**

```bash
cd /home/jaime/mcp-gateway/router

# Sandbox (P0 + P1 - 110+ testes)
go test ./internal/sandbox -v

# HTTP Handler (P1 - 10+ testes)
go test . -v

# Tudo junto com coverage
go test ./internal/sandbox . -v -cover
```

**Resultado esperado:**

```
PASS
ok      mcp-router/internal/sandbox     2.5s
PASS
ok      mcp-router      1.2s
```

---

## Documentação Completa

- **[SECURITY_TESTS.md](router/SECURITY_TESTS.md)** - P0 & P1 em detalhes (testes de segurança)
- **[TESTS.md](router/TESTS.md)** - Todos os 120+ testes do projeto (inclui runtime)
- **[README.md](README.md)** - Overview do projeto

---

## ✅ Confirmação P0 & P1 Completos

- ✅ Todos os requisitos P0 implementados e testados
- ✅ Todos os requisitos P1 implementados e testados
- ✅ 110+ testes passando
- ✅ Documentação atualizada
- ✅ Pronto para produção em ambiente controlado

**Próximos passos recomendados:**
1. Deploy P0 + P1 em ambiente staging
2. Depois iniciar P2 (container security)
3. Depois P3 (concurrency/logging)
