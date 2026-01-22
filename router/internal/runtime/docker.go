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

// Spawn executa a tool em container via `docker run -i`.
//
// Hardening mínimo (Prioridade 1.1), configurável por tool:
// - network: none|bridge  (default: none)
// - read_only: true|false (default: true)
// - no-new-privileges (sempre)
// - cap-drop=ALL (sempre)
//
// Observação (Lab):
// - ainda usamos docker.sock (alto privilégio). Cloudflare Access continua obrigatório.
func (DockerRuntime) Spawn(ctx context.Context, cfg *config.Config, tool config.Tool) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	env := append(os.Environ(),
		"WORKSPACE_ROOT="+cfg.WorkspaceRoot,
		"TOOLS_ROOT="+cfg.ToolsRoot,
	)

	// Defaults conservadores (somente para container)
	netMode := tool.DockerNetworkEffective()  // "none" | "bridge"
	readOnly := tool.ReadOnlyEffective()      // true/false

	args := []string{
		"run", "-i", "--rm",

		// Hardening base
		"--security-opt=no-new-privileges",
		"--cap-drop=ALL",
		"--network", netMode,
	}

	if readOnly {
		args = append(args, "--read-only")
		// tmpfs para permitir escrita temporária sem quebrar read-only (muitas imagens precisam)
		args = append(args, "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m")
		args = append(args, "--tmpfs", "/var/tmp:rw,noexec,nosuid,size=64m")
	}

	// Workspace mount (sandbox)
	args = append(args,
		"-v", fmt.Sprintf("%s:/workspaces", cfg.WorkspaceRoot),
	)

	// Imagem + args da tool
	args = append(args, tool.Image)
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
