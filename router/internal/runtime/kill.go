package runtime

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

func KillProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	if runtime.GOOS != "windows" {
		// 1. Enviamos SIGTERM para o GRUPO (-Pid). 
		// O sinal negativo indica que todos os processos no grupo recebem o sinal.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

		// 2. Janela de fôlego para o processo escrever o marker e fechar pipes.
		time.Sleep(150 * time.Millisecond)

		// 3. SIGKILL final para garantir que nada fique pendurado (zumbis).
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	} else {
		_ = cmd.Process.Kill()
	}
}

func KillOSProcess(p *os.Process) error {
	if p == nil { return nil }
	return p.Kill()
}