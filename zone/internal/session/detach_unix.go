//go:build !windows

package session

import (
	"os/exec"
	"syscall"
)

// setDetached puts the daemon in its own session so it survives the terminal
// closing (and is reparented to init when the parent exits).
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
