package runtime

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// KillProcess encerra um *exec.Cmd de forma best-effort.
// 1) tenta um shutdown "graceful" quando possível
// 2) depois força kill se necessário
//
// Observação: isso é usado pelo runner para cleanup quando o request/context encerra.
func KillProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// Primeiro tenta um encerramento mais suave no Unix.
	// Em Windows, Signals são diferentes; vamos direto para Kill().
	if runtime.GOOS != "windows" {
		_ = cmd.Process.Signal(syscall.SIGTERM)

		// Dá um tempo curto para o processo sair sozinho.
		done := make(chan struct{}, 1)
		go func() {
			_, _ = cmd.Process.Wait()
			done <- struct{}{}
		}()

		select {
		case <-done:
			return
		case <-time.After(500 * time.Millisecond):
			// continua para kill forçado
		}
	}

	// Força encerramento.
	_ = cmd.Process.Kill()

	// Best-effort: aguarda coleta do processo para evitar zombie (Unix).
	// Se já foi waitado em outro lugar, isso pode falhar; ignoramos.
	_, _ = cmd.Process.Wait()
}

// (Opcional) KillOSProcess existe caso você precise matar um *os.Process no futuro.
func KillOSProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
