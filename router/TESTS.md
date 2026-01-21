# Complete Test Suite - MCP Router

Documentação completa de todos os 11 arquivos de teste do projeto.

## Overview

| Camada | Arquivo | Foco | Testes |
|--------|---------|------|--------|
| **Main** | http_hardening_test.go | P1.1: HTTP Methods & Content-Type | 3 |
| **Main** | sse_headers_and_flush_test.go | P1.2: SSE Headers & Flushing | 1 |
| **Main** | sse_disconnect_kills_tool_test.go | P1.3: Disconnect → Tool Kill | 2 |
| **Sandbox** | sandbox.go (production code) | P0: Path & Name Validation | N/A |
| **Sandbox** | sandbox_test.go | P0: Path Traversal, Symlinks | 18 |
| **Sandbox** | command_injection_test.go | P0: Command Injection | 40+ |
| **Sandbox** | dos_test.go | P0: DoS Protection | 5+ |
| **Sandbox** | integration_test.go | P0: HTTP Route Integration | 8+ |
| **Sandbox** | sse_test.go | P0: SSE Client Disconnect | 1 |
| **Sandbox** | sse_headers_and_flush_test.go | P1.2: SSE Correctness | 8 |
| **Sandbox** | sse_disconnect_kills_tool_test.go | P1.3: Process Lifecycle | 3 |
| **Sandbox** | auth_header_regression_test.go | P1.4: Auth Headers Bypass Prevention | 4 |
| **Runtime** | native_runtime_test.go | Process Execution Safety | 3 |
| **Runtime** | docker_runtime_test.go | Container Execution Safety | 4 |

**Total: ~120 testes entre P0, P1 e runtime**

---

## Priority 0 (P0) - Critical Security

Estes testes garantem que vulnerabilidades críticas são bloqueadas antes de chegar à execução.

### Sandbox Package Tests

#### `internal/sandbox/sandbox_test.go` (18 testes)

**Validação de Caminhos (ValidatePath)**

Garante que requisições não escapam do workspace via:

- **Path Traversal (clássico)**: `../`, `..\\`, `/.`, `//`, `/./`
- **URL Encoding**: `%2e%2e%2f`, `%252e%252e%252f` (múltiplas camadas)
- **Symlink Absoluto**: `link → /etc/passwd` é bloqueado
- **Symlink Relativo**: `link → ../../outside` é bloqueado
- **Symlink Chain**: `link1 → link2 → ../outside` é bloqueado
- **Prefix Boundary**: `/ws` não pode acessar `/ws2` via HasPrefix bypass
- **Non-existent Targets**: Symlinks para caminhos inexistentes são validados

**Validação de Nomes (ValidateToolName)**

Restricões de caracteres para tool names:

- Rejeita: `/`, `\`, spaces, `..`, `%2f`, `%5c`, `%25`
- Apenas: alphanumeric + `-` e `_`
- Exemplos: `fs`, `git`, `my-tool` ✓ | `../../bin`, `tool;whoami` ✗

**Executar:**

```bash
go test ./internal/sandbox -run TestValidatePath -v
go test ./internal/sandbox -run TestValidateToolName -v
```

#### `internal/sandbox/command_injection_test.go` (40+ testes)

**Command Execution Safety**

Verifica que `exec.Command` (sem shell) trata todos os argumentos como literais:

- **Metacharacters**: `;`, `|`, `&&`, `||`, `&`, `>`, `<`, `>>`, `2>`, `|&`
- **Shell Variables**: `$VAR`, `${VAR}`, `$?`, `$!`, `$#`
- **Command Substitution**: `$(command)`, `` `command` ``
- **Quote Escaping**: Single/double quotes, backticks
- **Globbing**: `*`, `?`, `[...]`
- **Redirection**: `>file`, `<input`, `>>append`

Todos são tratados como argumentos literais, nunca interpretados.

**Executar:**

```bash
go test ./internal/sandbox -run TestCommand -v
```

#### `internal/sandbox/dos_test.go` (5+ testes)

**DoS Protection**

Proteções contra consumo de recursos:

- **Body Size Limit**: Máximo 1MB por request
- **Context Timeout**: Cada request tem timeout (default 30s)
- **SSE Streaming**: Buffer máximo 4MB por linha
- **Connection Handling**: Múltiplas conexões simultâneas limitadas
- **Memory Exhaustion**: Previne infinite streaming

**Executar:**

```bash
go test ./internal/sandbox -run DOS -v
```

#### `internal/sandbox/integration_test.go` (8+ testes)

**HTTP Route-Level Security**

Validação integrada no handler `/mcp/<tool>`:

- **Tool Name Validation**: Rejeita tool names inválidos (400)
- **Unknown Tool**: 404 para tools não registrados
- **Path Traversal in URL**: Bloqueia `../` em paths
- **Query Parameter Injection**: Não aceita path traversal em query params
- **Error Responses**: Status codes corretos (400, 404, 415)
- **Allowlist Enforcement**: Apenas tools em config.yaml são acessíveis

**Executar:**

```bash
go test ./internal/sandbox -run TestIntegration -v
```

#### `internal/sandbox/sse_test.go` (1 teste)

**SSE Client Disconnect Detection**

- **Context Cancellation**: Quando cliente desconecta, `ctx.Done()` é acionado
- **Process Cleanup**: Tool process recebe sinal de encerramento
- **No Resource Leak**: Goroutines são limpas corretamente

**Executar:**

```bash
go test ./internal/sandbox -run TestRequestContextCanceledOnClientDisconnect -v
```

---

## Priority 1 (P1) - Hardening & Correctness

Estes testes garantem que o servidor HTTP e streaming SSE funcionam corretamente e são hardened contra mau uso.

### HTTP Hardening

#### `http_hardening_test.go` (3 testes)

**Localização**: `/home/jaime/mcp-gateway/router/http_hardening_test.go` (package main)

**HTTP Methods Validation**

- **GET/POST Permitidos**: Não retornam 405
- **PUT/DELETE/PATCH/TRACE**: Retornam 405 Method Not Allowed
- **CONNECT/OPTIONS**: Também 405

**Content-Type Validation**

- **POST + JSON**: Válido, aceito
- **POST + JSON; charset=utf-8**: Válido
- **POST + application/xml**: 415 Unsupported Media Type
- **POST + text/plain**: 415
- **POST sem Content-Type**: 415
- **GET**: Content-Type não validado (GET sem body)

**Executar:**

```bash
cd router
go test . -run TestHTTPMethods -v
go test . -run TestContentType -v
```

### SSE Headers & Correctness

#### `sse_headers_and_flush_test.go` (8 testes)

**Localização**: Versão em `internal/sandbox/` + versão em `router/` (package main)

**Sandbox Version (internal/sandbox/):**

- **Content-Type**: Deve ser exatamente `text/event-stream`
- **Cache-Control**: Obrigatoriamente `no-cache`
- **Connection**: `keep-alive`
- **X-Accel-Buffering**: `no` (evita buffer em Nginx)
- **Flusher Interface**: ResponseWriter implementa http.Flusher
- **No Caching Headers**: Rejeita `Expires`, `Pragma: cache`

**Executar:**

```bash
go test ./internal/sandbox -run SSE -v
```

**Main Version (router/):**

Testes de integração no handler real de `/mcp/<tool>`.

**Executar:**

```bash
cd router
go test . -run TestSSEHeaders -v
```

### Process Lifecycle: Disconnect → Kill

#### `sse_disconnect_kills_tool_test.go` (3 testes)

**Localização**: Versão em `internal/sandbox/` + versão em `router/`

**Process Context Cancellation**

- **Client Disconnect**: Quando TCP fecha, server detecta
- **Context Done**: `<-ctx.Done()` é acionado no handler
- **Process Kill**: Tool process recebe SIGTERM (ou SIGKILL se não responder)
- **No Resource Leak**: Todas goroutines terminam, file descriptors fecham

**Cenário Real:**

1. Cliente (Cloudflare Tunnel) conecta ao `/mcp/<tool>` via SSE
2. Tool inicia e começa a processar
3. Tunnel desconecta ou cliente fecha browser
4. TCP close é detectado pelo server
5. Context é cancelado
6. `defer cancel()` mata o processo
7. Recursos são limpos

**Executar:**

```bash
go test ./internal/sandbox -run SSEDisconnect -v
cd router && go test . -run SSEDisconnect -v
```

### Auth Headers Regression

#### `auth_header_regression_test.go` (4 testes)

**Localização**: `internal/sandbox/auth_header_regression_test.go`

**Auth/Proxy Headers Never Bypass**

Testes-regressão documentam que:

- `X-Auth: ok` não permite tool inválido
- `Authorization: Bearer token` não bypassa validação
- `X-Forwarded-User: admin` não muda permissões
- `CF-Access-Authenticated: true` não autoriza paths proibidos

Mensagem: **Headers não validam, apenas validação de tool name/path o faz.**

**Executar:**

```bash
go test ./internal/sandbox -run TestAuth -v
```

---

## Runtime Tests

Testes de runtimes (Native Process e Docker Container).

### Native Runtime

#### `internal/runtime/native_runtime_test.go` (3 testes)

**Process Execution via os/exec**

- **Arguments Passed Literally**: Shell metacharacters não são interpretados
  - `; echo hacked` é um argumento, não um comando separado
  - `| cat /etc/passwd` é um argumento, não um pipe
  - `$(whoami)` é um argumento, não command substitution

- **Environment Variables**: WORKSPACE_ROOT, TOOLS_ROOT são setados corretamente

- **Context Cancellation**: Quando `ctx.Done()`, processo recebe sinal (SIGTERM)
  - Usa `KillProcess()` de `kill.go`
  - SIGTERM aguarda 500ms
  - Depois SIGKILL se necessário

**Executar:**

```bash
go test ./internal/runtime -run TestNativeRuntime -v
```

### Docker Runtime

#### `internal/runtime/docker_runtime_test.go` (4 testes)

**Container Execution via docker run**

- **Docker Args Building**: Constrói `docker run -i --rm -e WORKSPACE_ROOT=... image args...` corretamente

- **Arguments Passed Literally**: Mesmo que native, args não são interpretados (docker passa ao container)

- **Environment Variables**: WORKSPACE_ROOT, TOOLS_ROOT são setados no container

- **Context Cancellation**: Quando `ctx.Done()`, mata o container via `docker kill` ou signal

- **Image Pulling**: Valida que imagem é pulled antes de tentar rodar

**Executar:**

```bash
go test ./internal/runtime -run TestDockerRuntime -v
```

---

## Running Complete Test Suite

**Todos os testes:**

```bash
cd /home/jaime/mcp-gateway/router

# Sandbox (P0 + P1)
go test ./internal/sandbox -v

# Runtime (native + docker)
go test ./internal/runtime -v

# Main handler (HTTP hardening + SSE)
go test . -v

# Tudo junto
go test ./... -v
```

**Com cobertura:**

```bash
go test ./... -v -cover

# Cobertura detalhada por package
go test ./internal/sandbox -cover
go test ./internal/runtime -cover
```

**Testes específicos:**

```bash
# P0: Security
go test ./internal/sandbox -run "ValidatePath|ValidateToolName|Command|DOS|Integration" -v

# P1: Hardening
go test ./internal/sandbox -run "HTTP|SSE|Auth" -v
go test . -run "HTTPMethods|ContentType|SSEHeaders|SSEDisconnect" -v

# Runtime
go test ./internal/runtime -v
```

---

## Test File Organization

### `/home/jaime/mcp-gateway/router/internal/sandbox/`

Production + Tests:

- `sandbox.go` - ValidateToolName(), ValidatePath(), checkPathTraversal()
- `sandbox_test.go` - Path/name validation (P0)
- `command_injection_test.go` - Shell safety (P0)
- `dos_test.go` - Resource limits (P0)
- `integration_test.go` - HTTP routes (P0)
- `sse_test.go` - Client disconnect (P0)
- `sse_headers_and_flush_test.go` - SSE correctness (P1)
- `sse_disconnect_kills_tool_test.go` - Process lifecycle (P1)
- `auth_header_regression_test.go` - Auth bypass prevention (P1)

### `/home/jaime/mcp-gateway/router/internal/runtime/`

- `runtime.go` - Interface de runtime
- `native.go` - os/exec implementation
- `docker.go` - docker run implementation
- `kill.go` - Process termination (SIGTERM → SIGKILL)
- `native_runtime_test.go` - Process execution tests
- `docker_runtime_test.go` - Container execution tests

### `/home/jaime/mcp-gateway/router/`

- `main.go` - HTTP handler, SSE setup
- `http_hardening_test.go` - HTTP method/content-type validation (P1)
- `sse_headers_and_flush_test.go` - SSE header validation (P1)
- `sse_disconnect_kills_tool_test.go` - Process kill on disconnect (P1)

---

## Quick Reference

| Vulnerability | Test File | Test Name |
|---|---|---|
| Path Traversal | sandbox_test.go | TestValidatePath_PathTraversal |
| URL Encoding Bypass | sandbox_test.go | TestValidatePath_EncodingBypass |
| Symlink Escape | sandbox_test.go | TestValidatePath_SymlinkEscape |
| Symlink Chain | sandbox_test.go | TestValidatePath_RejectsSymlinkChainEscape |
| Command Injection | command_injection_test.go | TestCommand_* |
| Body Size DoS | dos_test.go | TestRequestBodySizeLimit |
| Shell Metacharacters | command_injection_test.go | TestCommand_* |
| HTTP Method Override | http_hardening_test.go | TestHTTPMethods_Hardening |
| Content-Type Bypass | http_hardening_test.go | TestContentTypeValidation |
| SSE Cache Bypass | sse_headers_and_flush_test.go | TestSSENoCache |
| Process Leak | sse_disconnect_kills_tool_test.go | TestSSEDisconnectKillsProcessContext |
| Auth Header Bypass | auth_header_regression_test.go | TestAuthHeadersBypassRegression |
| Env Variable Injection | native_runtime_test.go | TestNativeRuntime_Spawn_SetsWorkspaceAndToolsEnv |
| Docker Args Injection | docker_runtime_test.go | TestDockerRuntime_Spawn_BuildsExpectedDockerArgsAndPassesArgsLiterally |
