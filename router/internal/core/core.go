package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/runner"
	"mcp-router/internal/sandbox"
)

type LineWriter interface {
	WriteLine([]byte) error
}

type Service struct {
	cfg *config.Config
	r   *runner.Runner
}

func New(cfg *config.Config) *Service {
	return &Service{
		cfg: cfg,
		r:   runner.New(cfg),
	}
}

type ToolInfo struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
	Mode    string `json:"mode"`
}

// GET /mcp/tools (e stdio "tools/list" no futuro)
func (s *Service) ListTools(ctx context.Context) ([]ToolInfo, error) {
	_ = ctx
	out := make([]ToolInfo, 0, len(s.cfg.Tools))
	for name, t := range s.cfg.Tools {
		out = append(out, ToolInfo{
			Name:    name,
			Runtime: t.Runtime,
			Mode:    t.Mode,
		})
	}
	return out, nil
}

// StreamTool executa a tool (launcher), manda 1 input (linha JSON) e streama stdout linha a linha.
//
// Importante: este método monitora ctx.Done() e mata o processo ao cancelar (ex.: cliente SSE desconectou).
// Também valida toolName via sandbox, para que HTTP e stdio compartilhem a mesma regra.
func (s *Service) StreamTool(ctx context.Context, toolName string, inputJSON []byte, out LineWriter) error {
	if err := sandbox.ValidateToolName(toolName); err != nil {
		return fmt.Errorf("invalid tool name: %w", err)
	}

	tool, err := s.r.MustGetTool(toolName)
	if err != nil {
		return err
	}

	tctx, cancel := context.WithTimeout(ctx, tool.Timeout())
	defer cancel()

	p, err := s.r.Start(tctx, toolName, tool)
	if err != nil {
		return err
	}

	// Garante cleanup e também garante kill no cancelamento (cliente desconectou / timeout).
	done := make(chan struct{})
	go func() {
		select {
		case <-tctx.Done():
			_ = p.Close()
		case <-done:
		}
	}()
	defer close(done)
	defer func() { _ = p.Close() }()

	// garante input JSON válido; se vier vazio, manda {}
	if len(inputJSON) == 0 {
		inputJSON = []byte(`{}`)
	}
	if !json.Valid(inputJSON) {
		return fmt.Errorf("invalid input json")
	}

	// Escreve UMA linha no stdin e fecha (importante pro launcher finalizar).
	if err := writeJSONLineAndClose(p.Stdin(), inputJSON); err != nil {
		return fmt.Errorf("write stdin: %w", err)
	}

	// stdout streaming
	sc := bufio.NewScanner(p.Stdout())
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		// Se o contexto foi cancelado, finalize cedo.
		select {
		case <-tctx.Done():
			return tctx.Err()
		default:
		}

		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 {
			continue
		}
		if err := out.WriteLine(line); err != nil {
			// erro ao escrever (ex.: broken pipe no SSE) => caller desconectou
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read stdout: %w", err)
	}

	// espera fim do processo
	if err := p.Wait(); err != nil {
		return err
	}
	return nil
}

func writeJSONLineAndClose(w io.WriteCloser, b []byte) error {
	if len(b) == 0 {
		b = []byte(`{}`)
	}

	// garante newline
	if b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	if _, err := w.Write(b); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// util pra quando você quiser expor timeout também (opcional)
func (s *Service) ToolTimeout(name string) (time.Duration, bool) {
	t, ok := s.cfg.Tools[name]
	if !ok {
		return 0, false
	}
	return t.Timeout(), true
}
