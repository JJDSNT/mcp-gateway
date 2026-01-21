package runtime

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// KillProcess encerra um *exec.Cmd de forma best-effort.
// Importante: NÃO chama Wait() aqui para evitar corrida/double-wait com cmd.Wait()
// que já é chamado no fluxo normal do router.
func KillProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// Tenta graceful no Unix.
	if runtime.GOOS != "windows" {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		// Pequena janela para o processo reagir (ex: escrever marker e sair)
		time.Sleep(300 * time.Millisecond)
	}

	// Força encerramento (best-effort)
	_ = cmd.Process.Kill()
}

// (Opcional) KillOSProcess existe caso você precise matar um *os.Process no futuro.
func KillOSProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
