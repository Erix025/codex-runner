package machineup

import (
	"context"
	"errors"
	"testing"
	"time"

	"codex-runner/internal/codexremote/config"
	"codex-runner/internal/codexremote/machcheck"
	"codex-runner/internal/codexremote/sshutil"
)

func TestStartWithDaemonAlreadyHealthy(t *testing.T) {
	m := config.Machine{Name: "gpu1", SSH: "user@host", DaemonCmd: "start"}
	runCalled := 0
	res := startWith(
		context.Background(),
		m,
		func(ctx context.Context, sshTarget string, remoteCmd string) (sshutil.RunResult, error) {
			runCalled++
			return sshutil.RunResult{}, nil
		},
		func(ctx context.Context, mm config.Machine) machcheck.Status {
			return machcheck.Status{Name: mm.Name, SSHOK: true, DaemonOK: true}
		},
		func(d time.Duration) {},
		3,
	)

	if !res.OK {
		t.Fatalf("expected ok result, got %#v", res)
	}
	if runCalled != 0 {
		t.Fatalf("start command should not run when daemon is already healthy")
	}
	if res.Phase != "precheck" {
		t.Fatalf("phase = %q, want precheck", res.Phase)
	}
}

func TestStartWithFailsWhenSSHNotReady(t *testing.T) {
	m := config.Machine{Name: "gpu1", SSH: "user@host", DaemonCmd: "start"}
	res := startWith(
		context.Background(),
		m,
		func(ctx context.Context, sshTarget string, remoteCmd string) (sshutil.RunResult, error) {
			t.Fatalf("start command should not run when ssh precheck fails")
			return sshutil.RunResult{}, nil
		},
		func(ctx context.Context, mm config.Machine) machcheck.Status {
			return machcheck.Status{Name: mm.Name, SSHOK: false, DaemonOK: false, Error: "ssh not reachable"}
		},
		func(d time.Duration) {},
		3,
	)

	if res.OK {
		t.Fatalf("expected failure when ssh is down")
	}
	if res.Phase != "precheck" {
		t.Fatalf("phase = %q, want precheck", res.Phase)
	}
	if res.Error == "" {
		t.Fatalf("expected precheck error message")
	}
}

func TestStartWithStartErrorAndUnhealthyDaemon(t *testing.T) {
	m := config.Machine{Name: "gpu1", SSH: "user@host", DaemonCmd: "start"}
	checkCount := 0
	res := startWith(
		context.Background(),
		m,
		func(ctx context.Context, sshTarget string, remoteCmd string) (sshutil.RunResult, error) {
			return sshutil.RunResult{Stderr: "permission denied", Code: 255}, errors.New("exit status 255")
		},
		func(ctx context.Context, mm config.Machine) machcheck.Status {
			checkCount++
			if checkCount == 1 {
				return machcheck.Status{Name: mm.Name, SSHOK: true, DaemonOK: false, Error: "daemon not healthy"}
			}
			return machcheck.Status{Name: mm.Name, SSHOK: true, DaemonOK: false, Error: "daemon not healthy"}
		},
		func(d time.Duration) {},
		2,
	)

	if res.OK {
		t.Fatalf("expected failure when start command fails and daemon remains unhealthy")
	}
	if res.Phase != "start" {
		t.Fatalf("phase = %q, want start", res.Phase)
	}
	if res.Error == "" || res.Hint == "" {
		t.Fatalf("expected actionable error and hint, got %#v", res)
	}
}

func TestStartWithEventuallyHealthy(t *testing.T) {
	m := config.Machine{Name: "gpu1", SSH: "user@host", DaemonCmd: "start"}
	checkCount := 0
	res := startWith(
		context.Background(),
		m,
		func(ctx context.Context, sshTarget string, remoteCmd string) (sshutil.RunResult, error) {
			return sshutil.RunResult{Stdout: "started", Code: 0}, nil
		},
		func(ctx context.Context, mm config.Machine) machcheck.Status {
			checkCount++
			switch checkCount {
			case 1:
				return machcheck.Status{Name: mm.Name, SSHOK: true, DaemonOK: false, Error: "daemon not healthy"}
			case 2:
				return machcheck.Status{Name: mm.Name, SSHOK: true, DaemonOK: false, Error: "daemon not healthy"}
			default:
				return machcheck.Status{Name: mm.Name, SSHOK: true, DaemonOK: true}
			}
		},
		func(d time.Duration) {},
		5,
	)

	if !res.OK {
		t.Fatalf("expected success after health retries, got %#v", res)
	}
	if !res.After.DaemonOK {
		t.Fatalf("expected healthy daemon after retries")
	}
	if checkCount < 3 {
		t.Fatalf("expected multiple health checks, got %d", checkCount)
	}
}
