package runtime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	"router/internal/config"
)

type DockerRuntime struct{}

func (DockerRuntime) Spawn(ctx context.Context, cfg *config.Config, tool config.Tool) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
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
		return nil, nil, nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}

	// log stderr
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Printf("[tool stderr] %s", sc.Text())
		}
	}()

	return cmd, stdin, stdout, nil
}
