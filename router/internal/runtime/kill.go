package runtime

import (
	"errors"
	"os"
	"os/exec"
	goRuntime "runtime"
	"syscall"
	"time"
)

// KillProcess tenta encerrar o processo de forma graciosa e, se necessário, força a morte.
// Em Unix-like:
//  1) SIGTERM no grupo (process tree inteira)
//  2) espera até graceTimeout o processo morrer
//  3) SIGKILL no grupo como fallback
//
// Em Windows:
//  - fallback para Process.Kill (não há SIGTERM/PGID da mesma forma)
func KillProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// Windows: não dá pra usar sinais/PGID como em Unix.
	if goRuntime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		return
	}

	pid := cmd.Process.Pid

	// Descobre o PGID real; não assuma que PGID == PID.
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		// Fallback: tenta no processo direto.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		waitForExit(cmd.Process, 500*time.Millisecond)
		_ = cmd.Process.Kill()
		return
	}

	// 1) SIGTERM no grupo inteiro (process tree).
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	// 2) Espera graciosa: dá tempo pro helper escrever marker e sair.
	if waitForExit(cmd.Process, 800*time.Millisecond) {
		return
	}

	// 3) Força: SIGKILL no grupo.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)

	// Espera um pouco pra evitar deixar processo em estado esquisito (best effort).
	_ = waitForExit(cmd.Process, 500*time.Millisecond)
}

// KillOSProcess mantém compatibilidade com usos antigos.
// Em Unix tenta SIGTERM + SIGKILL no PID (não no grupo). Em Windows chama Kill().
func KillOSProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	if goRuntime.GOOS == "windows" {
		return p.Kill()
	}

	// Best-effort gracioso
	_ = p.Signal(syscall.SIGTERM)
	if waitForExit(p, 500*time.Millisecond) {
		return nil
	}
	return p.Kill()
}

// waitForExit retorna true se o processo já tiver saído dentro do timeout.
// Implementação por polling usando Signal(0).
func waitForExit(p *os.Process, timeout time.Duration) bool {
	if p == nil {
		return true
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Em Unix, Signal(0) não envia sinal, só testa existência/permissão.
		err := p.Signal(syscall.Signal(0))
		if err != nil {
			// Se não existe mais, ótimo.
			// Nota: alguns erros podem ser permissão; aqui, para processo filho nosso,
			// o mais comum é "os: process already finished".
			if errors.Is(err, os.ErrProcessDone) {
				return true
			}
			// Em geral, erro aqui indica que o processo não está mais rodando.
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
