package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"mcp-router/internal/config"
)

type DockerRuntime struct{}

func (DockerRuntime) Spawn(ctx context.Context, cfg *config.Config, tool config.Tool) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	env := append(os.Environ(),
		"WORKSPACE_ROOT="+cfg.WorkspaceRoot,
		"TOOLS_ROOT="+cfg.ToolsRoot,
	)

	args := []string{
		"run", "-i", "--rm",
		"-v", fmt.Sprintf("%s:/workspaces", cfg.WorkspaceRoot),
		tool.Image,
	}
	args = append(args, tool.Args...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = env

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
