package machcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"codex-runner/internal/codexremote/config"
	"codex-runner/internal/codexremote/sshutil"
)

type Status struct {
	Name       string `json:"name"`
	SSHOK      bool   `json:"ssh_ok"`
	DaemonOK   bool   `json:"daemon_ok"`
	LatencyMS  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
	CheckedAt  string `json:"checked_at"`
	DaemonPort int    `json:"daemon_port"`
	DaemonAddr string `json:"daemon_addr,omitempty"`
}

var addrHealthCheck = func(ctx context.Context, addr string) bool {
	req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(addr, "/")+"/health", nil)
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode/100 == 2
}

func Check(ctx context.Context, m config.Machine) Status {
	start := time.Now()
	st := Status{
		Name:       m.Name,
		DaemonPort: m.DaemonPort,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}

	hasSSH := strings.TrimSpace(m.SSH) != ""
	hasAddr := strings.TrimSpace(m.Addr) != ""
	if !hasSSH && !hasAddr {
		st.Error = "machine.ssh or machine.addr is required for check"
		st.LatencyMS = time.Since(start).Milliseconds()
		return st
	}

	sshErr := ""
	if hasSSH {
		res, err := sshutil.RunSSH(ctx, m.SSH, "echo ok")
		if err == nil && strings.TrimSpace(res.Stdout) == "ok" {
			st.SSHOK = true
		} else {
			sshErr = strings.TrimSpace(res.Stderr)
			if sshErr == "" {
				sshErr = "ssh not reachable"
			}
		}
	}

	if st.SSHOK {
		healthCmd := fmt.Sprintf("curl -fsS http://127.0.0.1:%d/health", m.DaemonPort)
		res2, err := sshutil.RunSSH(ctx, m.SSH, healthCmd)
		if err == nil {
			var tmp map[string]any
			if json.Unmarshal([]byte(res2.Stdout), &tmp) == nil {
				st.DaemonOK = true
			}
		}
	}

	if !st.DaemonOK && hasAddr {
		// Optional: if addr is configured (e.g. VSCode forward already), check directly too.
		if addrHealthCheck(ctx, m.Addr) {
			st.DaemonOK = true
			st.DaemonAddr = m.Addr
		}
	}

	if !st.DaemonOK {
		if sshErr != "" {
			st.Error = sshErr
		} else {
			st.Error = "daemon not healthy"
		}
	}
	st.LatencyMS = time.Since(start).Milliseconds()
	return st
}
