package runtime

import "os/exec"

func KillProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}
