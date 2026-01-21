# Tools Directory

This directory contains **Native Runtime MCP tools** executed directly by the `mcp-router` via STDIO.

Any executable placed here can be exposed by the gateway by adding an entry to `config.yaml`.

---

## Requirements

A tool must:

- Read MCP messages from **STDIN**
- Write MCP responses to **STDOUT**
- Use **JSON Lines (JSONL)** format (one JSON object per line)
- Flush output immediately

---

## Example (Python)

`echo.py`

```python
import sys, json

for line in sys.stdin:
    msg = json.loads(line)
    print(json.dumps({"result": msg, "done": True}), flush=True)
```

Make executable (optional):

```bash
chmod +x echo.py
```

---

## config.yaml Example

```yaml
tools:
  echo:
    runtime: native
    mode: launcher
    cmd: "python3"
    args: ["/tools/echo.py"]
```

Or directly executable:

```yaml
tools:
  echo:
    runtime: native
    mode: launcher
    cmd: "/tools/echo.py"
```

---

## Use Cases

Native tools are ideal for:

- Rapid prototyping
- Custom scripts
- Debugging the MCP bridge
- Lightweight transformations
- Local experiments

---

## Notes

- Tools run inside the `mcp-router` container namespace
- Access is restricted to `/workspaces` by default
- Avoid running untrusted scripts

---

## When To Use Container Runtime Instead

Prefer Container Runtime when:

- Tool has heavy dependencies
- Isolation is required
- Tool comes from external sources
- You need reproducible environments

---

This directory is intentionally simple.  
The gateway treats any compatible executable as a valid MCP tool.

---
