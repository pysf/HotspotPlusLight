//go:build windows

package update

import (
	"os/exec"
	"syscall"
)

// detach starts the child without a console and in its own process group.
func detach(cmd *exec.Cmd) {
	const detachedProcess = 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
