# Pendências Técnicas (Estado Atual)


## 🔴 Prioridade 1 — Segurança mínima para tunelamento (DONE)

> **Objetivo:** permitir exposição via Cloudflare Tunnel/Access sem riscos óbvios.

### 1. Hardening básico do Container Runtime (configurável por tool)
- Flags mínimas de segurança:
  - `network: bridge | none`
  - `read_only: true | false`
- Default conservador **apenas para containers**
- Native runtime permanece inalterado

### 2. Limite de concorrência por tool
- Evitar:
  - fork bomb acidental
  - exaustão de CPU/memória via requests paralelos
- Implementação simples:
  - semaphore por tool
  - `max_concurrent` configurável (default: 1–2)

### 3. Fail-safe de execução (invariante)
- Garantir que **todo processo**:
  - possui timeout
  - é finalizado em cancelamento
- Tornar isso uma **regra documentada do core**

---

## 🟠 Prioridade 2 — Operação segura e previsível (DONE)

> **Objetivo:** debugar e operar o gateway com confiança.

### 4. Logging estruturado mínimo
- Migrar para `log/slog`
- Campos fixos:
  - `tool`
  - `runtime`
  - `request_id`
  - `duration`
  - `error`

### 5. Semântica clara de erro em SSE
- Regras explícitas:
  - erro **antes** do primeiro evento → HTTP error
  - erro **após** início do streaming → log + `event:error` opcional
- Evitar múltiplos eventos de erro por request

### 6. Health e readiness endpoints
- `/healthz`: processo vivo
- `/readyz`: config carregada + runtimes disponíveis

---

## 🟡 Prioridade 3 — Conforto e evolução do laboratório

> **Objetivo:** melhorar DX e preparar features futuras.

### 7. Rate limiting leve
- Por tool ou global
- Opcional quando rodando atrás de Cloudflare Access

### 8. Políticas de workspace
- Read-only vs read-write
- Mapeamento mais fino de volumes

### 9. Execução em modo daemon
- Processos persistentes
- Pooling / reuse
- Multiplexação de requests

---

## Fora de escopo imediato
- Métricas detalhadas
- Scheduling inteligente
- Auto-scaling
- Sistema de plugins

---

## Resumo

Para exposição via tunnel com segurança mínima, o **próximo ciclo essencial** é:

> **Hardening básico do Docker + limite de concorrência por tool**

Todo o restante pode evoluir incrementalmente depois.
