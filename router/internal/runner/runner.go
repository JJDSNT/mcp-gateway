package runner

import (
	"context"
	"fmt"

	"router/internal/config"
	"router/internal/runtime"
)

type Runner struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Runner {
	return &Runner{cfg: cfg}
}

func (r *Runner) Start(ctx context.Context, tool config.Tool) (Process, error) {
	rt, err := runtime.FromTool(tool)
	if err != nil {
		return nil, err
	}

	cmd, stdin, stdout, err := rt.Spawn(ctx, r.cfg, tool)
	if err != nil {
		return nil, err
	}

	p := &execProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		closer: func() { runtime.KillProcess(cmd) },
		waiter: func() error { return cmd.Wait() },
	}

	return p, nil
}

func (r *Runner) MustGetTool(name string) (config.Tool, error) {
	tool, ok := r.cfg.Tools[name]
	if !ok {
		return config.Tool{}, fmt.Errorf("unknown tool: %s", name)
	}
	return tool, nil
}
