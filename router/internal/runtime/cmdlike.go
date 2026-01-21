package runtime

// CmdLike é a menor interface que o Runner precisa.
// *exec.Cmd satisfaz isso.
type CmdLike interface {
	Wait() error
}
