package runner

import (
	"context"
	"fmt"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/observability/logging"
	"mcp-router/internal/runtime"
)

type Runner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Runner {
	return &Runner{cfg: cfg}
}

func (r *Runner) Start(ctx context.Context, toolName string, tool config.Tool) (Process, error) {
	start := time.Now()

	// request-scoped logger (transport injeta no ctx)
	log := logging.LoggerFromContext(ctx).With(
		logging.Tool(toolName),
		logging.Runtime(tool.Runtime), // runtime definido no config
		logging.RequestID(logging.RequestIDFromContext(ctx)),
	)

	// Resolve runtime backend a partir do tool (native/container)
	rt, err := runtime.FromTool(tool)
	if err != nil {
		log.Error("failed to resolve runtime",
			logging.Err(err),
			logging.DurationMs(time.Since(start).Milliseconds()),
		)
		return nil, err
	}

	log.Info("spawning tool process",
		// úteis pra debug operacional
		logging.String("mode", tool.Mode),
	)

	cmd, stdin, stdout, stderr, err := rt.Spawn(ctx, r.cfg, tool)
	if err != nil {
		log.Error("failed to spawn tool process",
			logging.Err(err),
			logging.DurationMs(time.Since(start).Milliseconds()),
		)
		return nil, err
	}

	// Observabilidade leve: PID quando disponível
	if cmd != nil && cmd.Process != nil {
		log.Debug("process started", logging.Int("pid", cmd.Process.Pid))
	} else {
		log.Debug("process started")
	}

	p := &execProcess{
		toolName: toolName,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		closeFn:  func() { runtime.KillProcess(cmd) },
		waitFn:   func() error { return cmd.Wait() },
	}

	// stderr pump é “owned” pelo process; termina com ctx/process
	p.startStderrPump(ctx)

	// Opcional: loga que o stderr pump iniciou (útil em debug)
	log.Debug("stderr pump started")

	return p, nil
}

func (r *Runner) MustGetTool(name string) (config.Tool, error) {
	tool, ok := r.cfg.Tools[name]
	if !ok {
		return config.Tool{}, fmt.Errorf("unknown tool: %s", name)
	}
	return tool, nil
}
