P0 — Bloqueia incidentes sérios (faça primeiro)

Workspace sandbox: bloquear path traversal (inclui encoding)

../, %2e%2e%2f, %252e%252e%252f, ..\\, //, /.

Esperado: rejeitar antes de chamar qualquer tool.

Workspace sandbox: bloquear “symlink escape”

Dentro do workspace criar link -> / e tentar acessar link/etc/passwd.

Esperado: rejeitar (ou resolver realpath e validar).

Allowlist rígida de tools + validação do <tool> na rota

/mcp/<tool> só aceita tool existente no YAML.

<tool> com /, .., %2f, \, espaços → 400/404.

Garantir que execução é “sem shell” (anti command injection)

Assegurar que cmd/args viram exec.Command(cmd, args...) (ou equivalente) e nunca sh -c.

Teste garante que caracteres tipo ; | && $( ) em args não executam nada extra.

DoS básico: limites e timeouts

Body gigante → falhar rápido

Tool travada → timeout/cancelamento previsível

Conexão SSE lenta → não consumir memória infinito (stream sem buffer).

P1 — Segurança “porque vai pra internet” (logo depois)

Hardening de HTTP: métodos e headers

Só aceitar métodos esperados (provável POST/GET conforme implementação).

Rejeitar Content-Type inválido quando aplicável.

SSE correctness + anti-cache

Content-Type: text/event-stream, Cache-Control: no-cache, flush/stream sem buffer.

Teste valida que resposta realmente “streama” (não só devolve tudo no final).

Cancelar tool quando cliente desconecta

Se o cliente dropa a conexão do túnel, o processo/container deve ser encerrado (evita leak/DoS).

Não confiar em headers de “auth” internos

Mesmo com Cloudflare Access na frente, garantir que o router não tem bypass acidental (ex.: “se veio header X então ok”).

Aqui é um “teste-regressão”: falha se alguém introduzir shortcut.

P2 — Container runtime (alto risco, mas pode vir depois do básico)

Testes de “container run” sem privilégios inesperados

Não permitir flags tipo --privileged, --pid=host, --net=host (a menos que explicitamente suportado).

Garantir volumes mínimos (workspace) e, se possível, read-only.

Garantir que o gateway não vaza segredos para containers

Env vars sensíveis não devem ser propagadas por padrão.

Teste específico do alerta docker.sock

Como o README já avisa, docker.sock dá poder de root: criar testes que falhem se alguém “facilitar” isso com mounts/flags extras.

P3 — Qualidade e observabilidade (bom ter)

Testes de concorrência (race)

Vários clientes batendo na mesma tool (principalmente daemon mode) sem corromper stream/mutex.

Testes de “daemon mode idle timeout”

Sobe tool, fica ociosa, deve encerrar; volta a receber request, deve subir de novo.

Logs/telemetria mínima

Garantir que erros relevantes aparecem (stderr, headers MCP) sem vazar payload sensível.