package runtime

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"router/internal/config"
)

type Runtime interface {
	Spawn(ctx context.Context, cfg *config.Config, tool config.Tool) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error)
}

func FromTool(tool config.Tool) (Runtime, error) {
	switch tool.Runtime {
	case "native":
		return NativeRuntime{}, nil
	case "container":
		return DockerRuntime{}, nil
	default:
		return nil, fmt.Errorf("invalid runtime: %s", tool.Runtime)
	}
}
