//go:build !windows

package service

import (
	"os"
	"os/exec"
	"syscall"
)

func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func gracefulStopExec(pid int) error {
	// Negative pid targets the process group on unix.
	return syscall.Kill(-pid, syscall.SIGTERM)
}

func forceStopExec(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
