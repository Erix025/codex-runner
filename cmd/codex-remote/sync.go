package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"codex-runner/internal/codexremote/sshutil"
	"codex-runner/internal/shared/jsonutil"
)

func syncCmd(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "push":
		syncPush(args[1:])
	case "pull":
		syncPull(args[1:])
	default:
		usage()
		os.Exit(2)
	}
}

func syncPush(args []string) {
	runSync("push", args)
}

func syncPull(args []string) {
	runSync("pull", args)
}

func runSync(mode string, args []string) {
	fs := flag.NewFlagSet("sync "+mode, flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	src := fs.String("src", "", "source path")
	dst := fs.String("dst", "", "destination path")
	deleteExtra := fs.Bool("delete", false, "delete extra files")
	excludes := multiFlag{}
	fs.Var(&excludes, "exclude", "exclude pattern (repeatable)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || strings.TrimSpace(*src) == "" || strings.TrimSpace(*dst) == "" {
		fmt.Fprintln(os.Stderr, "--machine, --src and --dst are required")
		os.Exit(2)
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		fmt.Fprintln(os.Stderr, "sync failed: local rsync is not installed")
		os.Exit(1)
	}

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(2)
	}
	m, ok := cfg.FindMachine(*machineName)
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown machine:", *machineName)
		os.Exit(2)
	}
	if strings.TrimSpace(m.SSH) == "" {
		fmt.Fprintln(os.Stderr, "machine.ssh is required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if probe, err := sshutil.RunSSH(ctx, m.SSH, "command -v rsync >/dev/null 2>&1"); err != nil {
		msg := strings.TrimSpace(probe.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(probe.Stdout)
		}
		if msg == "" {
			msg = err.Error()
		}
		fmt.Fprintln(os.Stderr, "sync failed: remote rsync probe failed:", msg)
		os.Exit(1)
	}

	rsyncArgs := []string{"-az", "--info=progress2"}
	if *deleteExtra {
		rsyncArgs = append(rsyncArgs, "--delete")
	}
	for _, ex := range excludes {
		rsyncArgs = append(rsyncArgs, "--exclude", ex)
	}
	sshCmd := append([]string{"ssh"}, sshutil.BuildSSHArgs(false, false)...)
	rsyncArgs = append(rsyncArgs, "-e", strings.Join(sshCmd, " "))

	srcSpec := strings.TrimSpace(*src)
	dstSpec := strings.TrimSpace(*dst)
	if mode == "push" {
		rsyncArgs = append(rsyncArgs, srcSpec, m.SSH+":"+dstSpec)
	} else {
		rsyncArgs = append(rsyncArgs, m.SSH+":"+srcSpec, dstSpec)
	}

	cmd := exec.Command("rsync", rsyncArgs...)
	stdoutTail := newTailBuffer(128 * 1024)
	stderrTail := newTailBuffer(128 * 1024)
	// Stream rsync output live to stderr so long-running sync does not look stuck,
	// while retaining bounded tails for JSON diagnostics.
	cmd.Stdout = io.MultiWriter(stdoutTail, os.Stderr)
	cmd.Stderr = io.MultiWriter(stderrTail, os.Stderr)
	err = cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	_ = jsonutil.WriteJSON(os.Stdout, map[string]any{
		"ok":      err == nil,
		"mode":    mode,
		"machine": m.Name,
		"src":     *src,
		"dst":     *dst,
		"delete":  *deleteExtra,
		"exclude": []string(excludes),
		"code":    code,
		"stdout":  stdoutTail.String(),
		"stderr":  stderrTail.String(),
	})
	if err != nil {
		os.Exit(1)
	}
}

type tailBuffer struct {
	limit int
	buf   []byte
}

func newTailBuffer(limit int) *tailBuffer {
	if limit < 0 {
		limit = 0
	}
	return &tailBuffer{limit: limit}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if t.limit == 0 || n == 0 {
		return n, nil
	}
	if n >= t.limit {
		t.buf = append(t.buf[:0], p[n-t.limit:]...)
		return n, nil
	}
	need := len(t.buf) + n - t.limit
	if need > 0 {
		copy(t.buf, t.buf[need:])
		t.buf = t.buf[:len(t.buf)-need]
	}
	t.buf = append(t.buf, p...)
	return n, nil
}

func (t *tailBuffer) String() string {
	if t == nil {
		return ""
	}
	return string(t.buf)
}
