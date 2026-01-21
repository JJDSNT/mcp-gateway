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

## O Que Este Projeto É

- Gateway de transporte MCP  
- Router de ferramentas MCP  
- Process manager (launcher + daemon)  
- Camada de abstração de runtime MCP  

---

## Arquitetura

```
Client
  |
HTTPS + Auth
  |
Cloudflare Tunnel
  |
Caddy (proxy)
  |
mcp-router (Go)
  |
HTTP/SSE <-> STDIO Bridge
  |
Tool Runtime
  |         \
Native      Container
(process)   (docker run)
  |
Workspace
```

---

## Features

- Bridge HTTP/SSE ⇄ STDIO (JSONL)
- Roteamento genérico `/mcp/<tool>`
- Launcher mode (spawn por request)
- Daemon mode (processo persistente + idle timeout)
- **Native Tool Runtime**
- **Container Tool Runtime**
- Workspace sandbox
- Observabilidade básica (stderr + headers MCP)
- Configuração externa via YAML

---

## Stack

- Go (mcp-router)
- Caddy (reverse proxy)
- Cloudflare Tunnel + Access
- Docker Compose
- WSL2 (Windows 10)

---

## Tool Runtimes

O gateway suporta dois modelos de execução:

### Native Runtime

- Tools executadas como processos locais via STDIO  
- Montadas via volume (`/tools`)  
- Ideal para desenvolvimento e prototipagem  
- Não exige rebuild do gateway  

### Container Runtime

- Tools executadas via `docker run -i`  
- Isolamento forte por tool  
- Permite uso direto de imagens MCP/Docker Hub  
- Ideal para sandboxing e ambientes mais realistas  

---

## Workspace Sandbox

- Root único: `/workspaces`
- Cada tool opera em subpastas
- Gateway valida paths (chroot-like)
- Evita acesso fora do sandbox

---

## Segurança: Sandbox Package

O package `router/internal/sandbox/` implementa a camada de validação de segurança. Ele contém:

### `sandbox.go`
Implementa as funções core de validação:

- **`ValidateToolName(name string) error`**
  - Valida nomes de ferramentas contra uma allowlist de caracteres seguros
  - Rejeita: `/`, `\`, spaces, `..`, `%2f`, `%5c`, `%25`, caracteres não-alfanuméricos (exceto `-` e `_`)
  - Exemplo: `git`, `fs`, `my-tool` ✓ | `../../bin`, `tool;whoami` ✗
  - Usado em `main.go` para validar o segmento `<tool>` da rota `/mcp/<tool>`

- **`ValidatePath(workspaceRoot, requestedPath string) (string, error)`**
  - Valida e resolve caminhos dentro do workspace
  - Detecta e bloqueia path traversal em múltiplos níveis de encoding
  - Trata symlinks com segurança:
    - Rejeita symlinks absolutos (ex: `→ /etc/passwd`)
    - Valida symlinks relativos (ex: `../../outside` é bloqueado)
    - Detecta cadeias de symlinks maliciosas (ex: `link1 → link2 → ../outside`)
  - Retorna o caminho absoluto resolvido se válido, erro caso contrário
  - Usado no handler MCP antes de executar qualquer operação com arquivos

- **`checkPathTraversal(path string) error`**
  - Helper que detecta padrões perigosos: `..`, `//`, `/.`, `/./`, etc.
  - Executado em cada componente do caminho separadamente

---

## Testes de Segurança (P0 - Crítico)

Os testes de segurança estão localizados em `router/internal/sandbox/` e cobrem as principais vulnerabilidades:

### `sandbox_test.go`
Valida a função `ValidatePath()` que garante que os caminhos solicitados não escapam do workspace. Testes incluem:
- **Path Traversal**: Bloqueia `../`, `..\\`, `//`, `/.` (e variações com encoding)
- **Encoding Bypass**: Detecta `%2e%2e%2f` (URL encoding), `%252e%252e%252f` (duplo encoding), etc.
- **Symlinks**: Rejeita symlinks absolutos e relativos que escapam do workspace
- **Symlink Chains**: Detecta cadeias de symlinks maliciosas (ex: `link1 → link2 → ../outside`)
- **Validação de Nomes**: Testa `ValidateToolName()` com caracteres inválidos (spaces, `/`, `\`, etc.)
- **Prefix Boundary**: Evita bypasses via colisão de prefixos (ex: `/ws` vs `/ws2`)

### `command_injection_test.go`
Verifica que comandos são executados com segurança sem interpretação de shell:
- **Command Separators**: Testa que `;`, `|`, `&&`, `||` não são interpretados como separadores
- **Shell Features**: Valida que `$()`, backticks, `$VAR`, redirecionamentos (`>`, `<`, `>>`) são tratados como literais
- **Quote Bypass**: Verifica que aspas não escapam do contexto da string
- Usa `exec.Command` (sem shell), garantindo que argumentos nunca são interpretados

### `dos_test.go`
Protege contra ataques de negação de serviço:
- **Body Size Limits**: Rejeita requests com corpo maior que 1MB
- **Context Timeouts**: Valida timeouts em requisições HTTP e execução de tools
- **SSE Streaming**: Testa limites de buffer (4MB por linha) para evitar consumo infinito de memória
- **Connection Limits**: Verifica que múltiplas conexões simultâneas são limitadas

### `integration_test.go`
Testa a integração dos validadores com as rotas HTTP:
- **Tool Name Validation**: Verifica que tool names inválidos são rejeitados na rota `/mcp/<tool>`
- **HTTP Path Traversal**: Testa que path traversal em query parameters é bloqueado
- **Error Responses**: Valida que erros de validação retornam status HTTP correto (400 Bad Request)
- **Allowlist Enforcement**: Confirma que apenas tools registrados em config.yaml são acessíveis

### `sse_test.go`
Valida o comportamento de streaming SSE (Server-Sent Events) e detecção de desconexão:
- **Client Disconnect**: Testa que o servidor detecta quando um cliente SSE se desconecta
- **Context Cancellation**: Verifica que o `context.Context` é cancelado imediatamente após desconexão
- **Resource Cleanup**: Garante que resources não vazam quando um cliente abandona a conexão
- **DoS Prevention**: Evita que clientes maliciosos façam múltiplas conexões rápidas para esgotar recursos

## Bridge HTTP/SSE ↔ STDIO

Fluxo:

```
HTTP/SSE Client
      |
   mcp-router
      |
   spawn tool
      |
 STDIN / STDOUT
```

- Entrada: JSON → STDIN  
- Saída: JSONL → SSE  
- Streaming sem buffer  
- Fila/mutex por processo  

---

## Configuração (config.yaml)

```yaml
workspace_root: /workspaces
tools_root: /tools

tools:
  fs:
    runtime: container
    mode: launcher
    image: mcp/filesystem
    args: ["/workspaces"]

  git:
    runtime: native
    mode: launcher
    cmd: "npx"
    args: ["-y", "@modelcontextprotocol/server-git", "/workspaces"]

  my-script:
    runtime: native
    mode: daemon
    cmd: "/tools/meu-script.sh"
```

---

## Reverse Proxy (Caddyfile)

```caddy
mcp.seudominio.com {

  handle_path /mcp/* {
    reverse_proxy mcp-router:8080 {
      transport http {
        versions 1.1
      }
      flush_interval -1
    }
  }
}
```

---

## Quick Start

```bash
docker compose up -d
```

Acesso:

```
https://mcp.seudominio.com/mcp/fs
https://mcp.seudominio.com/mcp/git
```

---

## Security Note — Docker Socket (Container Runtime)

> ⚠️ **Aviso Importante**

Ao utilizar **Container Runtime**, o `mcp-router` precisa acessar o Docker daemon (ex: `/var/run/docker.sock`) para executar tools via `docker run`.

Isso implica que:

- O gateway passa a ter **privilégios equivalentes a root no host**
- Uma tool maliciosa pode criar containers arbitrários ou montar volumes sensíveis

### Recomendações (Lab)

- Use Container Runtime apenas em ambiente controlado  
- Prefira Native Runtime para desenvolvimento rápido  
- Não exponha o gateway publicamente sem:
  - Cloudflare Access ativo
  - firewall adequado
  - controle de acesso por identidade  

### Alternativas Futuras

- Docker rootless / Podman rootless  
- Runner sidecar isolado  
- Workers dedicados  
- MicroVMs (Firecracker)  

---

## Projetos Relacionados

Docker MCP Gateway  
https://github.com/docker/mcp-gateway  

IBM ContextForge  
https://ibm.github.io/mcp-context-forge/  

aarora79 MCP Gateway  
https://github.com/aarora79/mcp-gateway  

LobeHub MCP Gateway  
https://lobehub.com/mcp/common-creation-mcp-gateway  

WunderGraph MCP Gateway (experimental)  
https://www.infracloud.io/blogs/mcp-gateway/

---

## Roadmap Técnico (Lab)

- Hot reload de config.yaml  
- Workspace scoping por tool  
- Pool de processos daemon  
- Rate limiting por tool  
- Metrics endpoint  
- Health checks  
