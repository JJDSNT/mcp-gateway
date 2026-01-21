# Security Tests - MCP Router

## Overview

Os testes de segurança estão localizados em `internal/sandbox/` e cobrem as principais vulnerabilidades (P0 - Crítico).

Execute todos os testes com:

```bash
cd router
go test ./internal/sandbox -v
```

---

## Sandbox Package

O package `internal/sandbox/` implementa a camada de validação de segurança.

### `sandbox.go`

Implementa as funções core de validação:

#### `ValidateToolName(name string) error`

- Valida nomes de ferramentas contra uma allowlist de caracteres seguros
- Rejeita: `/`, `\`, spaces, `..`, `%2f`, `%5c`, `%25`, caracteres não-alfanuméricos (exceto `-` e `_`)
- Exemplo: `git`, `fs`, `my-tool` ✓ | `../../bin`, `tool;whoami` ✗
- Usado em `main.go` para validar o segmento `<tool>` da rota `/mcp/<tool>`

#### `ValidatePath(workspaceRoot, requestedPath string) (string, error)`

- Valida e resolve caminhos dentro do workspace
- Detecta e bloqueia path traversal em múltiplos níveis de encoding
- Trata symlinks com segurança:
  - Rejeita symlinks absolutos (ex: `→ /etc/passwd`)
  - Valida symlinks relativos (ex: `../../outside` é bloqueado)
  - Detecta cadeias de symlinks maliciosas (ex: `link1 → link2 → ../outside`)
- Retorna o caminho absoluto resolvido se válido, erro caso contrário
- Usado no handler MCP antes de executar qualquer operação com arquivos

#### `checkPathTraversal(path string) error`

- Helper que detecta padrões perigosos: `..`, `//`, `/.`, `/./`, etc.
- Executado em cada componente do caminho separadamente

---

## Test Files

### `sandbox_test.go`

Valida a função `ValidatePath()` que garante que os caminhos solicitados não escapam do workspace.

**Testes incluem:**

- **Path Traversal**: Bloqueia `../`, `..\\`, `//`, `/.` (e variações com encoding)
- **Encoding Bypass**: Detecta `%2e%2e%2f` (URL encoding), `%252e%252e%252f` (duplo encoding), etc.
- **Symlinks**: Rejeita symlinks absolutos e relativos que escapam do workspace
- **Symlink Chains**: Detecta cadeias de symlinks maliciosas (ex: `link1 → link2 → ../outside`)
- **Validação de Nomes**: Testa `ValidateToolName()` com caracteres inválidos (spaces, `/`, `\`, etc.)
- **Prefix Boundary**: Evita bypasses via colisão de prefixos (ex: `/ws` vs `/ws2`)

**Executar:**

```bash
go test ./internal/sandbox -run TestValidatePath -v
go test ./internal/sandbox -run TestValidateToolName -v
```

### `command_injection_test.go`

Verifica que comandos são executados com segurança sem interpretação de shell.

**Testes incluem:**

- **Command Separators**: Testa que `;`, `|`, `&&`, `||` não são interpretados como separadores
- **Shell Features**: Valida que `$()`, backticks, `$VAR`, redirecionamentos (`>`, `<`, `>>`) são tratados como literais
- **Quote Bypass**: Verifica que aspas não escapam do contexto da string
- Usa `exec.Command` (sem shell), garantindo que argumentos nunca são interpretados

**Executar:**

```bash
go test ./internal/sandbox -run TestCommand -v
```

### `dos_test.go`

Protege contra ataques de negação de serviço.

**Testes incluem:**

- **Body Size Limits**: Rejeita requests com corpo maior que 1MB
- **Context Timeouts**: Valida timeouts em requisições HTTP e execução de tools
- **SSE Streaming**: Testa limites de buffer (4MB por linha) para evitar consumo infinito de memória
- **Connection Limits**: Verifica que múltiplas conexões simultâneas são limitadas

**Executar:**

```bash
go test ./internal/sandbox -run TestDOS -v
```

### `integration_test.go`

Testa a integração dos validadores com as rotas HTTP.

**Testes incluem:**

- **Tool Name Validation**: Verifica que tool names inválidos são rejeitados na rota `/mcp/<tool>`
- **HTTP Path Traversal**: Testa que path traversal em query parameters é bloqueado
- **Error Responses**: Valida que erros de validação retornam status HTTP correto (400 Bad Request)
- **Allowlist Enforcement**: Confirma que apenas tools registrados em config.yaml são acessíveis

**Executar:**

```bash
go test ./internal/sandbox -run TestIntegration -v
```

### `sse_test.go`

Valida o comportamento de streaming SSE (Server-Sent Events) e detecção de desconexão.

**Testes incluem:**

- **Client Disconnect**: Testa que o servidor detecta quando um cliente SSE se desconecta
- **Context Cancellation**: Verifica que o `context.Context` é cancelado imediatamente após desconexão
- **Resource Cleanup**: Garante que resources não vazam quando um cliente abandona a conexão
- **DoS Prevention**: Evita que clientes maliciosos façam múltiplas conexões rápidas para esgotar recursos

**Executar:**

```bash
go test ./internal/sandbox -run TestSSE -v
```

---

## Coverage Summary

| Vulnerability | Protection | Test File |
|---|---|---|
| Path Traversal (`../`) | Blocked at validation layer | sandbox_test.go |
| URL Encoding Bypass | Multi-layer encoding detection | sandbox_test.go |
| Symlink Escape | Absolute symlink rejection | sandbox_test.go |
| Symlink Chain Escape | Recursive symlink resolution | sandbox_test.go |
| Command Injection | exec.Command (no shell) | command_injection_test.go |
| Shell Metacharacters | Treated as literals | command_injection_test.go |
| Body Size DoS | 1MB limit enforced | dos_test.go |
| Timeout DoS | Context timeouts | dos_test.go |
| Buffer Exhaustion | 4MB per line limit | dos_test.go |
| Client Disconnect | Context cancellation | sse_test.go |
| Resource Leak | Cleanup on disconnect | sse_test.go |

---

## Running All Tests

```bash
cd router

# Run all sandbox tests with verbose output
go test ./internal/sandbox -v

# Run with coverage
go test ./internal/sandbox -v -cover

# Run specific test
go test ./internal/sandbox -run TestValidatePath_RejectsSymlinkChainEscape -v
```

---

## Adding New Tests

1. Identify the vulnerability category (path, command, DoS, SSE)
2. Add test to corresponding file
3. Follow naming: `Test<Category>_<Scenario>_<Outcome>`
4. Example: `TestValidatePath_RejectsDoubleEncoding`
5. Run `go test ./internal/sandbox -v` to verify
6. Update this documentation if adding new category

---

## Security Checklist

- [ ] All path requests validated with `ValidatePath()`
- [ ] All tool names validated with `ValidateToolName()`
- [ ] Commands executed with `exec.Command` (never shell)
- [ ] HTTP request body size limited
- [ ] Request timeouts enforced
- [ ] SSE client disconnection detected
- [ ] No symlink escapes possible
- [ ] No encoding bypasses possible
