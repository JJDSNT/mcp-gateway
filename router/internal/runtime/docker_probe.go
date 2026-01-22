package runtime

import (
	"context"
	"os/exec"
	"time"
)

func DockerReady(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(cctx, "docker", "version", "--format", "{{.Server.Version}}")
	return cmd.Run()
}
