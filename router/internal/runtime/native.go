package runtime

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"

	"mcp-router/internal/config"
)

type NativeRuntime struct{}

func (NativeRuntime) Spawn(ctx context.Context, cfg *config.Config, tool config.Tool) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	env := append(os.Environ(),
		"WORKSPACE_ROOT="+cfg.WorkspaceRoot,
		"TOOLS_ROOT="+cfg.ToolsRoot,
	)

	cmd := exec.CommandContext(ctx, tool.Cmd, tool.Args...)
	cmd.Env = env

	// P0: Crucial para o teste de Kill/Disconnect. 
	// Setpgid cria um novo ID de grupo de processos. 
	// Quando matarmos o PID com sinal negativo, o Kernel mata a árvore toda.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, err
	}

	return cmd, stdin, stdout, stderr, nil
}