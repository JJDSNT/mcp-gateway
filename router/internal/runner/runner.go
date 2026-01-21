package runner

import (
	"context"
	"fmt"

	"mcp-router/internal/config"
	"mcp-router/internal/runtime"
)

type Runner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Runner {
	return &Runner{cfg: cfg}
}

func (r *Runner) Start(ctx context.Context, toolName string, tool config.Tool) (Process, error) {
	rt, err := runtime.FromTool(tool)
	if err != nil {
		return nil, err
	}

	cmd, stdin, stdout, stderr, err := rt.Spawn(ctx, r.cfg, tool)
	if err != nil {
		return nil, err
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

	return p, nil
}

func (r *Runner) MustGetTool(name string) (config.Tool, error) {
	tool, ok := r.cfg.Tools[name]
	if !ok {
		return config.Tool{}, fmt.Errorf("unknown tool: %s", name)
	}
	return tool, nil
}
