package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
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
	if _, err := sshutil.RunSSH(ctx, m.SSH, "command -v rsync >/dev/null 2>&1"); err != nil {
		fmt.Fprintln(os.Stderr, "sync failed: remote rsync is not installed")
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
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
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
		"stdout":  stdout.String(),
		"stderr":  stderr.String(),
	})
	if err != nil {
		os.Exit(1)
	}
}
