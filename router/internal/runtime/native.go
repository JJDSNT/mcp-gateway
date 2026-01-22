package runtime

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"syscall"

	"mcp-router/internal/config"
)

type NativeRuntime struct{}

func (NativeRuntime) Spawn(
	ctx context.Context,
	cfg *config.Config,
	tool config.Tool,
) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {

	env := append(os.Environ(),
		"WORKSPACE_ROOT="+cfg.WorkspaceRoot,
		"TOOLS_ROOT="+cfg.ToolsRoot,
	)

	// IMPORTANTE:
	// NÃO usar exec.CommandContext aqui.
	// O cancel do ctx deve ser tratado explicitamente com KillProcess,
	// para garantir SIGTERM antes de SIGKILL.
	cmd := exec.Command(tool.Cmd, tool.Args...)
	cmd.Env = env

	// Cria um novo process group (necessário para matar a árvore inteira).
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

	log.Printf(
		"[native] starting tool cmd=%q args=%v",
		tool.Cmd,
		tool.Args,
	)

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, err
	}

	log.Printf(
		"[native] tool started pid=%d",
		cmd.Process.Pid,
	)

	// Observa cancelamento do contexto (disconnect, timeout, shutdown).
	// O Runner também faz isso, mas manter aqui ajuda a depurar e
	// protege contra usos fora do Runner.
	go func() {
		<-ctx.Done()

		log.Printf(
			"[native] ctx canceled for pid=%d, invoking KillProcess",
			cmd.Process.Pid,
		)

		// Fecha stdin para ferramentas que saem por EOF
		_ = stdin.Close()

		KillProcess(cmd)

		log.Printf(
			"[native] KillProcess finished for pid=%d",
			cmd.Process.Pid,
		)
	}()

	return cmd, stdin, stdout, stderr, nil
}
