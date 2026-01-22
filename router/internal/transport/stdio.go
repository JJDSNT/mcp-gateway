package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"mcp-router/internal/core"
)

// Protocolo de entrada (1 JSON por linha):
// {"id":"1","tool":"echo","input":{"hello":"world"}}
//
// Saídas (JSON lines):
// {"id":"1","event":"message","data":<linha json do stdout da tool>}
// {"id":"1","event":"done","data":{"ok":true}}
// {"id":"1","event":"error","data":{"error":"...", "detail":"..."}}

type Stdio struct {
	core *core.Service
	in   io.Reader
	out  io.Writer
	mu   sync.Mutex
}

type StdioRequest struct {
	ID    string          `json:"id,omitempty"`
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input,omitempty"`
}

func NewStdio(svc *core.Service) *Stdio {
	return &Stdio{
		core: svc,
		in:   os.Stdin,
		out:  os.Stdout,
	}
}

func (t *Stdio) Run(ctx context.Context) error {
	sc := bufio.NewScanner(t.in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := bytesTrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}

		var req StdioRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = t.emit(req.ID, "error", map[string]any{
				"error":  "invalid_json",
				"detail": err.Error(),
			})
			continue
		}
		if req.Tool == "" {
			_ = t.emit(req.ID, "error", map[string]any{"error": "missing_tool"})
			continue
		}
		if len(req.Input) == 0 {
			req.Input = json.RawMessage(`{}`)
		}

		w := &stdioWriter{id: req.ID, emitRaw: t.emitRaw}

		if err := t.core.StreamTool(ctx, req.Tool, req.Input, w); err != nil {
			_ = t.emit(req.ID, "error", map[string]any{
				"error":  "tool_failed",
				"detail": err.Error(),
			})
			continue
		}
		_ = t.emit(req.ID, "done", map[string]any{"ok": true})
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan stdin: %w", err)
	}
	return nil
}

// stdioWriter implementa core.LineWriter: cada linha de stdout vira um evento "message".
type stdioWriter struct {
	id      string
	emitRaw func(id, event string, data json.RawMessage) error
}

func (w *stdioWriter) WriteLine(line []byte) error {
	// a tool já imprime JSON por linha => data é esse JSON "raw"
	return w.emitRaw(w.id, "message", json.RawMessage(append([]byte(nil), line...)))
}

func (t *Stdio) emit(id, event string, payload any) error {
	b, _ := json.Marshal(payload)
	return t.emitRaw(id, event, json.RawMessage(b))
}

func (t *Stdio) emitRaw(id, event string, data json.RawMessage) error {
	resp := map[string]any{"event": event}
	if id != "" {
		resp["id"] = id
	}
	if data != nil {
		resp["data"] = json.RawMessage(data)
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	_, err = t.out.Write(append(b, '\n'))
	return err
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j {
		c := b[i]
		if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
			break
		}
		i++
	}
	for j > i {
		c := b[j-1]
		if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
			break
		}
		j--
	}
	return b[i:j]
}
