package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/observability/logging"
	"mcp-router/internal/runner"
	"mcp-router/internal/sandbox"
)

type LineWriter interface {
	WriteLine([]byte) error
}

type Service struct {
	cfg *config.Config
	r   *runner.Runner

	// Limite de concorrência por tool (Prioridade 1.2)
	semMu sync.Mutex
	sem   map[string]chan struct{}
}

func New(cfg *config.Config) *Service {
	return &Service{
		cfg: cfg,
		r:   runner.New(cfg),
		sem: make(map[string]chan struct{}),
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

// ErrToolBusy é retornado quando o limite de concorrência da tool foi atingido.
var ErrToolBusy = fmt.Errorf("tool is busy")

func (s *Service) toolSemaphore(toolName string, tool config.Tool) chan struct{} {
	s.semMu.Lock()
	defer s.semMu.Unlock()

	if ch, ok := s.sem[toolName]; ok {
		return ch
	}

	capacity := tool.MaxConc() // default conservador no config
	ch := make(chan struct{}, capacity)
	s.sem[toolName] = ch
	return ch
}

func acquireSemaphore(sem chan struct{}) error {
	// Fail-fast (evita fila infinita e fork-bomb por paralelismo)
	select {
	case sem <- struct{}{}:
		return nil
	default:
		return ErrToolBusy
	}
}

func releaseSemaphore(sem chan struct{}) {
	select {
	case <-sem:
	default:
	}
}

// StreamTool executa a tool (launcher), manda 1 input (linha JSON) e streama stdout linha a linha.
//
// Invariantes:
// - toolName validado via sandbox
// - toda execução tem timeout (Tool.Timeout())
// - processo é finalizado em cancelamento (ctx.Done())
func (s *Service) StreamTool(ctx context.Context, toolName string, inputJSON []byte, out LineWriter) (retErr error) {
	start := time.Now()

	baseLog := logging.LoggerFromContext(ctx)
	rid := logging.RequestIDFromContext(ctx)

	log := baseLog.With(
		logging.RequestID(rid),
		logging.Tool(toolName),
	)

	var runtimeName string

	defer func() {
		if retErr != nil {
			log.Error("tool execution failed",
				logging.Runtime(runtimeName),
				logging.DurationMs(time.Since(start).Milliseconds()),
				logging.Err(retErr),
			)
		} else {
			log.Info("tool execution completed",
				logging.Runtime(runtimeName),
				logging.DurationMs(time.Since(start).Milliseconds()),
			)
		}
	}()

	if err := sandbox.ValidateToolName(toolName); err != nil {
		return fmt.Errorf("invalid tool name: %w", err)
	}

	tool, err := s.r.MustGetTool(toolName)
	if err != nil {
		return err
	}

	runtimeName = tool.Runtime
	log = log.With(logging.Runtime(runtimeName))

	// Limite de concorrência por tool
	sem := s.toolSemaphore(toolName, tool)
	if err := acquireSemaphore(sem); err != nil {
		log.Warn("tool concurrency limit reached",
			logging.Err(err),
			slog.Int("max_concurrent", tool.MaxConc()),
		)
		return err
	}
	defer releaseSemaphore(sem)

	log.Info("tool execution started",
		slog.String("mode", tool.Mode),
		slog.Int("max_concurrent", tool.MaxConc()),
	)

	tctx, cancel := context.WithTimeout(ctx, tool.Timeout())
	defer cancel()

	p, err := s.r.Start(tctx, toolName, tool)
	if err != nil {
		return err
	}

	log.Debug("process started")

	// Garante kill no cancelamento + cleanup
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

	if len(inputJSON) == 0 {
		inputJSON = []byte(`{}`)
	}
	if !json.Valid(inputJSON) {
		return fmt.Errorf("invalid input json")
	}

	if err := writeJSONLineAndClose(p.Stdin(), inputJSON); err != nil {
		return fmt.Errorf("write stdin: %w", err)
	}

	sc := bufio.NewScanner(p.Stdout())
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var lines int64
	for sc.Scan() {
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
			return err
		}

		lines++
		if log.Enabled(tctx, slog.LevelDebug) && lines%200 == 0 {
			log.Debug("streaming progress", slog.Int64("lines_out", lines))
		}
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("read stdout: %w", err)
	}

	if err := p.Wait(); err != nil {
		return err
	}

	return nil
}

func writeJSONLineAndClose(w io.WriteCloser, b []byte) error {
	if len(b) == 0 {
		b = []byte(`{}`)
	}
	if b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	if _, err := w.Write(b); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (s *Service) ToolTimeout(name string) (time.Duration, bool) {
	t, ok := s.cfg.Tools[name]
	if !ok {
		return 0, false
	}
	return t.Timeout(), true
}
