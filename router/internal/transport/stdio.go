package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

type Core interface {
	StreamTool(ctx context.Context, toolName string, inputJSON []byte, out LineWriter) error
	ListTools(ctx context.Context) ([]any, error) // não usado agora; pode remover se quiser
}

type LineWriter interface {
	WriteLine([]byte) error
}

type Request struct {
	ID    string          `json:"id,omitempty"`
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

type Transport struct {
	core any // vamos receber *core.Service direto no main e usar interface mínima abaixo
	in   io.Reader
	out  io.Writer
	mu   sync.Mutex
}

type service interface {
	StreamTool(ctx context.Context, toolName string, inputJSON []byte, out LineWriter) error
}

func New(svc service) *Transport {
	return &Transport{
		core: svc,
		in:   os.Stdin,
		out:  os.Stdout,
	}
}

func (t *Transport) Run(ctx context.Context) error {
	svc := t.core.(service)

	sc := bufio.NewScanner(t.in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := bytesTrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = t.emit(req.ID, "error", map[string]any{"error": "invalid_json", "detail": err.Error()})
			continue
		}
		if req.Tool == "" {
			_ = t.emit(req.ID, "error", map[string]any{"error": "missing_tool"})
			continue
		}
		if len(req.Input) == 0 {
			req.Input = json.RawMessage(`{}`)
		}

		w := &stdoutWriter{id: req.ID, emitRaw: t.emitRaw}

		if err := svc.StreamTool(ctx, req.Tool, req.Input, w); err != nil {
			_ = t.emit(req.ID, "error", map[string]any{"error": "tool_failed", "detail": err.Error()})
			continue
		}
		_ = t.emit(req.ID, "done", map[string]any{"ok": true})
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan stdin: %w", err)
	}
	return nil
}

type stdoutWriter struct {
	id      string
	emitRaw func(id, event string, data json.RawMessage) error
}

func (w *stdoutWriter) WriteLine(line []byte) error {
	// tool já imprime JSON por linha => data é esse JSON
	return w.emitRaw(w.id, "message", json.RawMessage(append([]byte(nil), line...)))
}

func (t *Transport) emit(id, event string, payload any) error {
	b, _ := json.Marshal(payload)
	return t.emitRaw(id, event, json.RawMessage(b))
}

func (t *Transport) emitRaw(id, event string, data json.RawMessage) error {
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
