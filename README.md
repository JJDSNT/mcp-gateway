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
