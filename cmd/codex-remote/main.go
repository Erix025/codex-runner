package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"codex-runner/internal/codexremote/client"
	"codex-runner/internal/codexremote/config"
	"codex-runner/internal/codexremote/dashboard"
	"codex-runner/internal/codexremote/machcheck"
	"codex-runner/internal/codexremote/sshutil"
	"codex-runner/internal/shared/jsonutil"
	"codex-runner/internal/shared/selfupdate"
)

var version = "dev"
var runSSHFn = sshutil.RunSSH
var machineCheckFn = machcheck.Check

const defaultRemoteConfigPath = "~/.config/codex-remote/config.yaml"

type machineUpResponse struct {
	OK      bool   `json:"ok"`
	Stage   string `json:"stage,omitempty"`
	Message string `json:"message,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
	Code    int    `json:"code,omitempty"`
}

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "exec":
		execCmd(os.Args[2:])
	case "sync":
		syncCmd(os.Args[2:])
	case "machine":
		machineCmd(os.Args[2:])
	case "dashboard":
		dashboardCmd(os.Args[2:])
	case "update":
		updateCmd(os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "codex-remote: local CLI for codexd")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  codex-remote exec run   --machine <name> --cmd <string> [--project <id> --ref <ref>] [--cwd <path>] [--env KEY=VAL ...]")
	fmt.Fprintln(os.Stderr, "  codex-remote exec start --machine <name> --cmd <string> [--project <id> --ref <ref>] [--cwd <path>] [--env KEY=VAL ...]")
	fmt.Fprintln(os.Stderr, "  codex-remote exec result --machine <name> --id <exec_id>")
	fmt.Fprintln(os.Stderr, "  codex-remote exec logs --machine <name> --id <exec_id> [--stream stdout|stderr] [--tail 2000] [--tail-lines N] [--since RFC3339|10m] [--until RFC3339|10m]")
	fmt.Fprintln(os.Stderr, "  codex-remote exec watch --machine <name> --id <exec_id> [--stream stdout|stderr|both] [--poll 1s]")
	fmt.Fprintln(os.Stderr, "  codex-remote exec doctor --machine <name> [--json]")
	fmt.Fprintln(os.Stderr, "  codex-remote exec cancel --machine <name> --id <exec_id>")
	fmt.Fprintln(os.Stderr, "  codex-remote sync push --machine <name> --src <local> --dst <remote> [--delete] [--exclude PATTERN ...]")
	fmt.Fprintln(os.Stderr, "  codex-remote sync pull --machine <name> --src <remote> --dst <local> [--delete] [--exclude PATTERN ...]")
	fmt.Fprintln(os.Stderr, "  codex-remote machine check --machine <name>")
	fmt.Fprintln(os.Stderr, "  codex-remote machine list [--json] [--parallel 6] [--timeout 8s]")
	fmt.Fprintln(os.Stderr, "  codex-remote machine ls   [--json] [--parallel 6] [--timeout 8s]")
	fmt.Fprintln(os.Stderr, "  codex-remote machine up --machine <name>")
	fmt.Fprintln(os.Stderr, "  codex-remote machine ssh --machine <name> --cmd <string> [--tty]")
	fmt.Fprintln(os.Stderr, "  codex-remote dashboard [--listen 127.0.0.1:8787]")
	fmt.Fprintln(os.Stderr, "  codex-remote update [--check] [--yes]")
	fmt.Fprintln(os.Stderr, "  codex-remote version")
}

func configFlag(fs *flag.FlagSet) *string {
	return fs.String("config", defaultRemoteConfigPath, "config file path")
}

func loadConfig(path string) (config.Config, error) {
	created, p, err := config.EnsureDefaultConfig(path)
	if err != nil {
		return config.Config{}, err
	}
	if created {
		fmt.Fprintln(os.Stderr, "created default config:", p)
	}
	return config.Load(path)
}

func updateCmd(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "check latest release only")
	yes := fs.Bool("yes", false, "apply update without prompt")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	u := selfupdate.Updater{
		BinaryName:     "codex-remote",
		CurrentVersion: version,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	check, err := u.Check(ctx, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update check failed:", err)
		os.Exit(1)
	}
	if *checkOnly {
		_ = jsonutil.WriteJSON(os.Stdout, map[string]any{
			"binary":           "codex-remote",
			"current_version":  check.CurrentVersion,
			"latest_version":   check.LatestVersion,
			"comparable":       check.Comparable,
			"update_available": check.UpdateAvailable,
			"asset":            check.AssetName,
		})
		return
	}
	if check.Comparable && !check.UpdateAvailable {
		fmt.Fprintf(os.Stdout, "codex-remote is up to date (%s)\n", check.CurrentVersion)
		return
	}
	if !*yes {
		fmt.Fprintf(os.Stderr, "update codex-remote from %s to %s? use --yes to confirm\n", check.CurrentVersion, check.LatestVersion)
		os.Exit(2)
	}
	latest, err := u.Update(ctx, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update failed:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "updated codex-remote to %s\n", latest)
}

func execCmd(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "run":
		execRun(args[1:])
	case "start":
		execStart(args[1:])
	case "result":
		execResult(args[1:])
	case "logs":
		execLogs(args[1:])
	case "watch":
		execWatch(args[1:])
	case "doctor":
		execDoctor(args[1:])
	case "cancel":
		execCancel(args[1:])
	default:
		usage()
		os.Exit(2)
	}
}

func machineCmd(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "check":
		machineCheck(args[1:])
	case "list", "ls":
		machineList(args[1:])
	case "up":
		machineUp(args[1:])
	case "ssh":
		machineSSH(args[1:])
	default:
		usage()
		os.Exit(2)
	}
}

func execRun(args []string) {
	fs := flag.NewFlagSet("exec run", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	projectID := fs.String("project", "", "project id")
	ref := fs.String("ref", "", "git ref (required if project is set)")
	cmdStr := fs.String("cmd", "", "command string")
	cwd := fs.String("cwd", "", "working dir (relative or absolute)")
	envList := multiFlag{}
	fs.Var(&envList, "env", "environment variable KEY=VAL (repeatable)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || *cmdStr == "" {
		fmt.Fprintln(os.Stderr, "--machine and --cmd are required")
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
	cl, closer, tm, err := connectClientForExec(*m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}

	env := map[string]string{}
	for _, kv := range envList {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}

	req := client.ExecStartRequest{
		ProjectID: *projectID,
		Ref:       *ref,
		Cmd:       *cmdStr,
		Cwd:       *cwd,
		Env:       env,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cl.ExecRun(ctx, req, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if tm != nil {
		logTunnelEvent("exec_run", map[string]any{
			"machine":        tm.machine,
			"local_port":     tm.localPort,
			"tunnel_pid":     tm.tunnelPID,
			"health_latency": tm.healthLatency.String(),
			"retry_count":    tm.retryCount,
		})
	}
}

func execStart(args []string) {
	fs := flag.NewFlagSet("exec start", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	projectID := fs.String("project", "", "project id")
	ref := fs.String("ref", "", "git ref (required if project is set)")
	cmdStr := fs.String("cmd", "", "command string")
	cwd := fs.String("cwd", "", "working dir (relative or absolute)")
	envList := multiFlag{}
	fs.Var(&envList, "env", "environment variable KEY=VAL (repeatable)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || *cmdStr == "" {
		fmt.Fprintln(os.Stderr, "--machine and --cmd are required")
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
	cl, closer, tm, err := connectClientForExec(*m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}

	env := map[string]string{}
	for _, kv := range envList {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}

	req := client.ExecStartRequest{
		ProjectID: *projectID,
		Ref:       *ref,
		Cmd:       *cmdStr,
		Cwd:       *cwd,
		Env:       env,
	}
	out, err := execStartOnce(cl, req)
	if err != nil && tm != nil {
		latency, healthErr := checkHealth(cl)
		if healthErr != nil || isRetryableExecErr(err) {
			logTunnelEvent("exec_start", map[string]any{
				"error_source":   "tunnel",
				"error":          err.Error(),
				"machine":        tm.machine,
				"local_port":     tm.localPort,
				"tunnel_pid":     tm.tunnelPID,
				"health_latency": latency.String(),
				"retry_count":    1,
			})
			if closer != nil {
				closer()
			}
			cl2, closer2, tm2, rebuildErr := connectClientForExec(*m)
			if rebuildErr == nil {
				cl = cl2
				closer = closer2
				tm = tm2
				if closer != nil {
					defer closer()
				}
				out, err = execStartOnce(cl, req)
			} else {
				err = fmt.Errorf("tunnel error: %v (orig exec start error: %w)", rebuildErr, err)
			}
		} else {
			logTunnelEvent("exec_start", map[string]any{
				"error_source":   "codexd",
				"error":          err.Error(),
				"machine":        tm.machine,
				"local_port":     tm.localPort,
				"tunnel_pid":     tm.tunnelPID,
				"health_latency": latency.String(),
				"retry_count":    0,
			})
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if tm != nil {
		logTunnelEvent("exec_start", map[string]any{
			"machine":        tm.machine,
			"local_port":     tm.localPort,
			"exec_id":        out.ExecID,
			"tunnel_pid":     tm.tunnelPID,
			"health_latency": tm.healthLatency.String(),
			"retry_count":    tm.retryCount,
		})
	}
	_ = jsonutil.WriteJSON(os.Stdout, map[string]any{
		"exec_id":  out.ExecID,
		"machine":  m.Name,
		"status":   out.Status,
		"base_url": cl.BaseURL,
	})
}

func execResult(args []string) {
	fs := flag.NewFlagSet("exec result", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	execID := fs.String("id", "", "exec id")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || *execID == "" {
		fmt.Fprintln(os.Stderr, "--machine and --id are required")
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
	cl, closer, tm, err := connectClientForExec(*m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var b json.RawMessage
	err = withRetry(3, func() error {
		out, callErr := cl.ExecGet(ctx, *execID)
		if callErr != nil {
			return callErr
		}
		b = out
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if tm != nil {
		logTunnelEvent("exec_result", map[string]any{
			"machine":        tm.machine,
			"local_port":     tm.localPort,
			"exec_id":        *execID,
			"tunnel_pid":     tm.tunnelPID,
			"health_latency": tm.healthLatency.String(),
			"retry_count":    tm.retryCount,
		})
	}
	_, _ = os.Stdout.Write(b)
	if len(b) == 0 || b[len(b)-1] != '\n' {
		_, _ = os.Stdout.Write([]byte("\n"))
	}
}

func execLogs(args []string) {
	fs := flag.NewFlagSet("exec logs", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	execID := fs.String("id", "", "exec id")
	stream := fs.String("stream", "stdout", "stdout or stderr")
	tailN := fs.Int64("tail", 2000, "tail bytes")
	tailLines := fs.Int("tail-lines", 0, "tail lines")
	since := fs.String("since", "", "lower time bound (RFC3339 or relative like 10m)")
	until := fs.String("until", "", "upper time bound (RFC3339 or relative like 10m)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || *execID == "" {
		fmt.Fprintln(os.Stderr, "--machine and --id are required")
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
	cl, closer, tm, err := connectClientForExec(*m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sinceRFC3339, err := normalizeTimeBound(*since)
	if err != nil {
		fmt.Fprintln(os.Stderr, "--since:", err)
		os.Exit(2)
	}
	untilRFC3339, err := normalizeTimeBound(*until)
	if err != nil {
		fmt.Fprintln(os.Stderr, "--until:", err)
		os.Exit(2)
	}
	opts := client.ExecLogsOptions{
		Stream:    *stream,
		TailBytes: *tailN,
		TailLines: *tailLines,
		Since:     sinceRFC3339,
		Until:     untilRFC3339,
		Format:    "jsonl",
	}
	if err := withRetry(3, func() error {
		return cl.ExecLogs(ctx, *execID, opts, os.Stdout)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if tm != nil {
		logTunnelEvent("exec_logs", map[string]any{
			"machine":        tm.machine,
			"local_port":     tm.localPort,
			"exec_id":        *execID,
			"stream":         *stream,
			"tunnel_pid":     tm.tunnelPID,
			"health_latency": tm.healthLatency.String(),
			"retry_count":    tm.retryCount,
		})
	}
}

func execCancel(args []string) {
	fs := flag.NewFlagSet("exec cancel", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	execID := fs.String("id", "", "exec id")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || *execID == "" {
		fmt.Fprintln(os.Stderr, "--machine and --id are required")
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
	cl, closer, tm, err := connectClientForExec(*m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := cl.ExecCancel(ctx, *execID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if tm != nil {
		logTunnelEvent("exec_cancel", map[string]any{
			"machine":        tm.machine,
			"local_port":     tm.localPort,
			"exec_id":        *execID,
			"tunnel_pid":     tm.tunnelPID,
			"health_latency": tm.healthLatency.String(),
			"retry_count":    tm.retryCount,
		})
	}
	_, _ = os.Stdout.Write(b)
	if len(b) == 0 || b[len(b)-1] != '\n' {
		_, _ = os.Stdout.Write([]byte("\n"))
	}
}

func machineCheck(args []string) {
	fs := flag.NewFlagSet("machine check", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	st := machcheck.Check(ctx, *m)
	_ = jsonutil.WriteJSON(os.Stdout, st)
}

func machineUp(args []string) {
	fs := flag.NewFlagSet("machine up", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
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
	if strings.TrimSpace(m.SSH) == "" {
		fmt.Fprintln(os.Stderr, "machine.ssh is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, exitCode := runMachineUp(ctx, *m)
	_ = json.NewEncoder(os.Stdout).Encode(resp)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runMachineUp(ctx context.Context, m config.Machine) (machineUpResponse, int) {
	if strings.TrimSpace(m.SSH) == "" {
		return machineUpResponse{
			OK:      false,
			Stage:   "validate",
			Message: "machine.ssh is required",
			Hint:    "set machine.ssh in config and retry",
		}, 2
	}

	preCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	pre := machineCheckFn(preCtx, m)
	cancel()
	if pre.DaemonOK {
		return machineUpResponse{
			OK:      true,
			Stage:   "precheck",
			Message: "daemon is already healthy",
		}, 0
	}
	if !pre.SSHOK {
		msg := strings.TrimSpace(pre.Error)
		if msg == "" {
			msg = "ssh not reachable"
		}
		return machineUpResponse{
			OK:      false,
			Stage:   "precheck",
			Message: msg,
			Hint:    "fix SSH connectivity first, then rerun `codex-remote machine check --machine " + m.Name + "`",
		}, 1
	}

	res, err := runSSHFn(ctx, m.SSH, m.DaemonCmd)
	if err != nil {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = err.Error()
		}
		return machineUpResponse{
			OK:      false,
			Stage:   "start",
			Message: msg,
			Hint:    "verify machine.daemon_cmd and remote shell env, then inspect /tmp/codexd.log on remote host",
			Stdout:  res.Stdout,
			Stderr:  res.Stderr,
			Code:    res.Code,
		}, 1
	}

	var lastErr string
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
		st := machineCheckFn(checkCtx, m)
		checkCancel()
		if st.DaemonOK {
			return machineUpResponse{
				OK:      true,
				Stage:   "verify",
				Message: "daemon started and health check passed",
				Stdout:  res.Stdout,
				Stderr:  res.Stderr,
				Code:    res.Code,
			}, 0
		}
		lastErr = strings.TrimSpace(st.Error)
	}
	if lastErr == "" {
		lastErr = "daemon not healthy after start command"
	}
	return machineUpResponse{
		OK:      false,
		Stage:   "verify",
		Message: lastErr,
		Hint:    "check remote daemon logs (/tmp/codexd.log) and run `codex-remote machine check --machine " + m.Name + "`",
		Stdout:  res.Stdout,
		Stderr:  res.Stderr,
		Code:    res.Code,
	}, 1
}

func machineList(args []string) {
	fs := flag.NewFlagSet("machine list", flag.ExitOnError)
	cfgPath := configFlag(fs)
	jsonOut := fs.Bool("json", false, "output json")
	timeout := fs.Duration("timeout", 8*time.Second, "per-machine check timeout")
	parallel := fs.Int("parallel", 6, "max parallel machine checks")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(2)
	}

	st := checkAllMachines(cfg.Machines, *timeout, *parallel)
	summary := machineListSummary(st)
	if *jsonOut {
		_ = jsonutil.WriteJSON(os.Stdout, map[string]any{
			"total":      summary.Total,
			"ssh_ok":     summary.SSHOK,
			"daemon_ok":  summary.DaemonOK,
			"failed":     summary.Failed,
			"checked_at": time.Now().UTC().Format(time.RFC3339Nano),
			"machines":   st,
		})
		return
	}

	if err := writeMachineListTable(os.Stdout, st); err != nil {
		fmt.Fprintln(os.Stderr, "failed to write machine table:", err)
		os.Exit(1)
	}
	fmt.Fprintf(
		os.Stdout,
		"\nsummary: total=%d ssh_ok=%d daemon_ok=%d failed=%d\n",
		summary.Total,
		summary.SSHOK,
		summary.DaemonOK,
		summary.Failed,
	)
}

func checkAllMachines(machines []config.Machine, timeout time.Duration, parallel int) []machcheck.Status {
	if parallel < 1 {
		parallel = 1
	}
	out := make([]machcheck.Status, len(machines))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	for i := range machines {
		wg.Add(1)
		m := machines[i]
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			out[idx] = machcheck.Check(ctx, m)
		}(i)
	}
	wg.Wait()
	return out
}

type machineSummary struct {
	Total    int
	SSHOK    int
	DaemonOK int
	Failed   int
}

func machineListSummary(statuses []machcheck.Status) machineSummary {
	s := machineSummary{Total: len(statuses)}
	for _, st := range statuses {
		if st.SSHOK {
			s.SSHOK++
		}
		if st.DaemonOK {
			s.DaemonOK++
		}
		if !st.DaemonOK || strings.TrimSpace(st.Error) != "" {
			s.Failed++
		}
	}
	return s
}

func writeMachineListTable(w io.Writer, statuses []machcheck.Status) error {
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tSSH\tDAEMON\tLATENCY_MS\tERROR"); err != nil {
		return err
	}
	for _, st := range statuses {
		ssh := "down"
		if st.SSHOK {
			ssh = "ok"
		}
		daemon := "down"
		if st.DaemonOK {
			daemon = "ok"
		}
		errMsg := strings.TrimSpace(st.Error)
		if errMsg == "" {
			errMsg = "-"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", st.Name, ssh, daemon, st.LatencyMS, errMsg); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func machineSSH(args []string) {
	fs := flag.NewFlagSet("machine ssh", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	cmdStr := fs.String("cmd", "", "remote command string")
	tty := fs.Bool("tty", false, "request tty (-tt)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || strings.TrimSpace(*cmdStr) == "" {
		fmt.Fprintln(os.Stderr, "--machine and --cmd are required")
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
	if strings.TrimSpace(m.SSH) == "" {
		fmt.Fprintln(os.Stderr, "machine.ssh is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := sshutil.RunSSHWithOptions(ctx, m.SSH, *cmdStr, true, *tty)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"ok":            err == nil,
		"ssh":           m.SSH,
		"forward_agent": true,
		"tty":           *tty,
		"stdout":        res.Stdout,
		"stderr":        res.Stderr,
		"code":          res.Code,
	})
}

func dashboardCmd(args []string) {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	cfgPath := configFlag(fs)
	listen := fs.String("listen", "127.0.0.1:8787", "listen address")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load config:", err)
		os.Exit(2)
	}
	s := dashboard.New(cfg)
	srv := &http.Server{
		Addr:              *listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintln(os.Stderr, "dashboard listening on http://"+*listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func connectClient(m config.Machine) (*client.Client, func(), error) {
	if m.Addr != "" {
		return client.New(m.Addr, m.Token), nil, nil
	}
	if m.SSH == "" {
		return nil, nil, fmt.Errorf("machine %s has neither addr nor ssh", m.Name)
	}
	startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Second)
	fwd, err := sshutil.StartLocalForward(startCtx, m.SSH, "127.0.0.1", "127.0.0.1", m.DaemonPort)
	startCancel()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create ssh forward: %w", err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", fwd.LocalPort)
	return client.New(base, m.Token), func() { _ = fwd.Close() }, nil
}

type tunnelMeta struct {
	machine       string
	localPort     int
	tunnelPID     int
	healthLatency time.Duration
	retryCount    int
}

func connectClientForExec(m config.Machine) (*client.Client, func(), *tunnelMeta, error) {
	if !m.UseDirectAddr {
		cl, closer, err := connectClient(m)
		return cl, closer, nil, err
	}
	if strings.TrimSpace(m.SSH) == "" {
		return nil, nil, nil, fmt.Errorf("tunnel error: machine %s requires machine.ssh for direct addr mode", m.Name)
	}
	maxAttempts := 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		startCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tunnel, err := sshutil.EnsureTunnel(startCtx, m.Name, m.SSH, 0, m.DaemonPort)
		cancel()
		if err != nil {
			lastErr = err
			if attempt < maxAttempts {
				backoff := time.Duration(1<<(attempt-1)) * 250 * time.Millisecond
				time.Sleep(backoff)
				continue
			}
			break
		}
		cl := client.New(tunnel.Addr(), m.Token)
		latency, healthErr := checkHealth(cl)
		if healthErr == nil {
			meta := &tunnelMeta{
				machine:       m.Name,
				localPort:     tunnel.LocalPort,
				tunnelPID:     tunnel.PID,
				healthLatency: latency,
				retryCount:    attempt - 1,
			}
			return cl, func() { _ = tunnel.Close() }, meta, nil
		}
		_ = tunnel.Close()
		lastErr = healthErr
		if attempt < maxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * 250 * time.Millisecond
			time.Sleep(backoff)
		}
	}
	return nil, nil, nil, fmt.Errorf("tunnel error: unable to establish healthy direct tunnel for machine %s: %w", m.Name, lastErr)
}

func checkHealth(cl *client.Client) (time.Duration, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := cl.Health(ctx)
	return time.Since(start), err
}

func logTunnelEvent(event string, fields map[string]any) {
	fields["event"] = event
	fields["mode"] = "direct_addr_tunnel"
	b, err := json.Marshal(fields)
	if err != nil {
		return
	}
	fmt.Fprintln(os.Stderr, string(b))
}

func execStartOnce(cl *client.Client, req client.ExecStartRequest) (client.ExecStartResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return cl.ExecStart(ctx, req)
}

func isRetryableExecErr(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection reset by peer") {
		return true
	}
	if strings.Contains(msg, "connection refused") {
		return true
	}
	if strings.Contains(msg, "broken pipe") {
		return true
	}
	if strings.Contains(msg, "eof") {
		return true
	}
	if strings.Contains(msg, "timeout") {
		return true
	}
	if strings.Contains(msg, "read |0: file already closed") {
		return true
	}
	if strings.Contains(msg, "file already closed") {
		return true
	}
	return false
}
