//go:build windows

package session

import (
	"os/exec"
	"syscall"
)

// setDetached starts the daemon in its own process group, detached from the
// parent console, so it survives the terminal closing.
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
	}
}
