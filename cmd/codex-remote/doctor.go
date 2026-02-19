package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"codex-runner/internal/codexremote/machcheck"
	"codex-runner/internal/codexremote/sshutil"
	"codex-runner/internal/shared/jsonutil"
)

func execDoctor(args []string) {
	fs := flag.NewFlagSet("exec doctor", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	jsonOut := fs.Bool("json", false, "json output")
	timeout := fs.Duration("timeout", 15*time.Second, "ssh timeout")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" {
		fmt.Fprintln(os.Stderr, "--machine is required")
		os.Exit(2)
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

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	st := machcheck.Check(ctx, *m)
	type check struct {
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail,omitempty"`
	}
	checks := []check{
		{Name: "ssh", OK: st.SSHOK, Detail: st.Error},
		{Name: "codexd_health", OK: st.DaemonOK, Detail: st.Error},
	}
	tools := []string{"python", "pixi", "rg", "nvidia-smi", "rsync"}
	for _, tool := range tools {
		cctx, ccancel := context.WithTimeout(context.Background(), *timeout)
		res, err := sshutil.RunSSH(cctx, m.SSH, "command -v "+tool+" >/dev/null 2>&1")
		ccancel()
		ok := err == nil && res.Code == 0
		detail := ""
		if !ok {
			detail = tool + " not found"
		}
		checks = append(checks, check{Name: tool, OK: ok, Detail: detail})
	}
	overall := true
	hints := make([]string, 0)
	for _, c := range checks {
		if c.OK {
			continue
		}
		overall = false
		switch c.Name {
		case "ssh":
			hints = append(hints, "check SSH key/auth and host reachability")
		case "codexd_health":
			hints = append(hints, "run `codex-remote machine up --machine "+m.Name+"` and re-check")
		default:
			hints = append(hints, "install "+c.Name+" on remote machine")
		}
	}
	out := map[string]any{
		"machine":    m.Name,
		"overall_ok": overall,
		"checks":     checks,
		"hints":      hints,
	}
	if *jsonOut {
		_ = jsonutil.WriteJSON(os.Stdout, out)
		if !overall {
			os.Exit(1)
		}
		return
	}
	_ = jsonutil.WriteJSON(os.Stdout, out)
	if !overall {
		os.Exit(1)
	}
}
