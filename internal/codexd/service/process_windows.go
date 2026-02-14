//go:build windows

package service

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureCmd(cmd *exec.Cmd) {}

func gracefulStopExec(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(os.Interrupt); err == nil {
		return nil
	}
	return p.Kill()
}

func forceStopExec(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return !errors.Is(err, os.ErrProcessDone)
}
