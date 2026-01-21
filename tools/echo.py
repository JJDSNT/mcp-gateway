# tools/echo.py
import sys, json

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
    except json.JSONDecodeError:
        print(json.dumps({"error": "invalid json", "done": True}), flush=True)
        continue

    # ecoa a mensagem para validar STDIO->SSE
    print(json.dumps({"tool": "echo", "result": msg, "done": True}), flush=True)
