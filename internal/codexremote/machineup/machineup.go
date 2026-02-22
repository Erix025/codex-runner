package machineup

import (
	"context"
	"strings"
	"time"

	"codex-runner/internal/codexremote/config"
	"codex-runner/internal/codexremote/machcheck"
	"codex-runner/internal/codexremote/sshutil"
)

type Result struct {
	OK      bool             `json:"ok"`
	Phase   string           `json:"phase"`
	Message string           `json:"message"`
	Error   string           `json:"error,omitempty"`
	Hint    string           `json:"hint,omitempty"`
	Stdout  string           `json:"stdout,omitempty"`
	Stderr  string           `json:"stderr,omitempty"`
	Code    int              `json:"code"`
	Before  machcheck.Status `json:"before"`
	After   machcheck.Status `json:"after"`
}

type runSSHFunc func(ctx context.Context, sshTarget string, remoteCmd string) (sshutil.RunResult, error)
type checkFunc func(ctx context.Context, m config.Machine) machcheck.Status
type sleepFunc func(d time.Duration)

func Start(ctx context.Context, m config.Machine) Result {
	return startWith(ctx, m, sshutil.RunSSH, machcheck.Check, time.Sleep, 5)
}

func startWith(
	ctx context.Context,
	m config.Machine,
	runSSH runSSHFunc,
	check checkFunc,
	sleep sleepFunc,
	maxChecks int,
) Result {
	if maxChecks < 1 {
		maxChecks = 1
	}
	out := Result{
		Phase:   "precheck",
		Message: "machine up precheck",
	}

	if strings.TrimSpace(m.SSH) == "" {
		out.Error = "machine.ssh is required"
		out.Hint = "set machine.ssh in config, then run `codex-remote machine check --machine " + m.Name + "`"
		return out
	}
	if strings.TrimSpace(m.DaemonCmd) == "" {
		out.Error = "machine.daemon_cmd is required"
		out.Hint = "set daemon_cmd in config to start codexd on the remote machine"
		return out
	}

	before := check(ctx, m)
	out.Before = before
	out.After = before
	if before.DaemonOK {
		out.OK = true
		out.Message = "daemon already healthy"
		return out
	}
	if !before.SSHOK {
		out.Error = firstNonEmpty(before.Error, "ssh check failed before start")
		out.Hint = "fix SSH connectivity first, then retry `codex-remote machine up --machine " + m.Name + "`"
		return out
	}

	out.Phase = "start"
	out.Message = "starting daemon via ssh command"
	runRes, runErr := runSSH(ctx, m.SSH, m.DaemonCmd)
	out.Stdout = strings.TrimSpace(runRes.Stdout)
	out.Stderr = strings.TrimSpace(runRes.Stderr)
	out.Code = runRes.Code

	out.Phase = "verify"
	out.Message = "verifying daemon health"
	after := check(ctx, m)
	for i := 1; i < maxChecks && !after.DaemonOK && ctx.Err() == nil; i++ {
		sleep(1 * time.Second)
		after = check(ctx, m)
	}
	out.After = after

	if after.DaemonOK {
		out.OK = true
		if runErr != nil {
			out.Message = "daemon is healthy, but start command reported an error"
			out.Error = firstNonEmpty(out.Stderr, runErr.Error())
		} else {
			out.Message = "daemon is healthy"
		}
		return out
	}

	out.OK = false
	if runErr != nil {
		out.Phase = "start"
		out.Message = "failed to execute daemon start command"
		out.Error = firstNonEmpty(out.Stderr, runErr.Error())
	} else if ctx.Err() != nil {
		out.Message = "timed out while waiting for daemon health"
		out.Error = ctx.Err().Error()
	} else {
		out.Message = "daemon did not become healthy after start"
		out.Error = firstNonEmpty(after.Error, "daemon health check failed")
	}
	out.Hint = failureHint(m.Name, out.Phase)
	return out
}

func failureHint(machineName string, phase string) string {
	if phase == "start" {
		return "verify daemon_cmd and remote permissions; then run `codex-remote machine check --machine " + machineName + "`"
	}
	return "inspect remote logs (for example `/tmp/codexd.log`) and then run `codex-remote machine check --machine " + machineName + "`"
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return "unknown error"
}
