//go:build !windows

package update

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session, so closing the terminal does not stop it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
