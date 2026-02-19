package sshutil

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Forward struct {
	LocalPort int
	cmd       *exec.Cmd
}

type Tunnel struct {
	Machine    string
	SSHTarget  string
	LocalHost  string
	LocalPort  int
	RemoteHost string
	RemotePort int
	PID        int
}

func StartLocalForward(ctx context.Context, sshTarget string, localHost string, remoteHost string, remotePort int) (*Forward, error) {
	if localHost == "" {
		localHost = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(localHost, "0"))
	if err != nil {
		return nil, err
	}
	localPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	args := []string{
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-N",
		"-L", fmt.Sprintf("%s:%d:%s:%d", localHost, localPort, remoteHost, remotePort),
		sshTarget,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Best-effort wait for the forward to become active.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(localHost, strconv.Itoa(localPort)), 150*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return &Forward{LocalPort: localPort, cmd: cmd}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = "ssh port forward did not become ready"
	}
	return nil, fmt.Errorf("%s", msg)
}

func (f *Forward) Close() error {
	if f == nil || f.cmd == nil || f.cmd.Process == nil {
		return nil
	}
	_ = f.cmd.Process.Kill()
	_, _ = f.cmd.Process.Wait()
	return nil
}

func EnsureTunnel(ctx context.Context, machine string, sshTarget string, localPort int, remotePort int) (*Tunnel, error) {
	localHost := "127.0.0.1"
	remoteHost := "127.0.0.1"
	if localPort == 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort(localHost, "0"))
		if err != nil {
			return nil, err
		}
		localPort = ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
	}
	if remotePort == 0 {
		remotePort = 7337
	}

	args := []string{
		"-f",
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-L", fmt.Sprintf("%s:%d:%s:%d", localHost, localPort, remoteHost, remotePort),
		sshTarget,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}

	if err := waitTunnelReady(localHost, localPort, 2*time.Second); err != nil {
		return nil, err
	}
	pid := lookupTunnelPID(localPort)
	return &Tunnel{
		Machine:    machine,
		SSHTarget:  sshTarget,
		LocalHost:  localHost,
		LocalPort:  localPort,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
		PID:        pid,
	}, nil
}

func (t *Tunnel) Addr() string {
	return fmt.Sprintf("http://%s:%d", t.LocalHost, t.LocalPort)
}

func (t *Tunnel) Close() error {
	if t == nil {
		return nil
	}
	if t.PID > 0 {
		proc, err := os.FindProcess(t.PID)
		if err == nil && proc != nil {
			if err := proc.Signal(syscall.SIGTERM); err == nil {
				return nil
			}
		}
	}
	pattern := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", t.LocalPort, t.RemotePort)
	_ = exec.Command("pkill", "-f", pattern).Run()
	return nil
}

func waitTunnelReady(localHost string, localPort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(localHost, strconv.Itoa(localPort)), 150*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("ssh tunnel did not become ready")
}

func lookupTunnelPID(localPort int) int {
	cmd := exec.Command("lsof", "-nP", "-tiTCP:"+strconv.Itoa(localPort), "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
	return pid
}

type RunResult struct {
	Stdout string
	Stderr string
	Code   int
}

func RunSSH(ctx context.Context, sshTarget string, remoteCmd string) (RunResult, error) {
	return RunSSHWithOptions(ctx, sshTarget, remoteCmd, false, false)
}

func RunSSHWithOptions(ctx context.Context, sshTarget string, remoteCmd string, forwardAgent bool, tty bool) (RunResult, error) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
	}
	if forwardAgent {
		args = append(args, "-A")
	}
	if tty {
		args = append(args, "-tt")
	}
	args = append(args, sshTarget, remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 255
		}
	}
	return RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   code,
	}, err
}
