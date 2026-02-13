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

func Check(ctx context.Context, m config.Machine) Status {
	start := time.Now()
	st := Status{
		Name:       m.Name,
		DaemonPort: m.DaemonPort,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}

	if m.SSH == "" {
		st.Error = "machine.ssh is required for check"
		st.LatencyMS = time.Since(start).Milliseconds()
		return st
	}

	res, err := sshutil.RunSSH(ctx, m.SSH, "echo ok")
	if err != nil || strings.TrimSpace(res.Stdout) != "ok" {
		st.Error = strings.TrimSpace(res.Stderr)
		if st.Error == "" {
			st.Error = "ssh not reachable"
		}
		st.LatencyMS = time.Since(start).Milliseconds()
		return st
	}
	st.SSHOK = true

	healthCmd := fmt.Sprintf("curl -fsS http://127.0.0.1:%d/health", m.DaemonPort)
	res2, err := sshutil.RunSSH(ctx, m.SSH, healthCmd)
	if err == nil {
		var tmp map[string]any
		if json.Unmarshal([]byte(res2.Stdout), &tmp) == nil {
			st.DaemonOK = true
		}
	}
	if !st.DaemonOK && m.Addr != "" {
		// Optional: if addr is configured (e.g. VSCode forward already), check directly too.
		req, _ := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(m.Addr, "/")+"/health", nil)
		hc := &http.Client{Timeout: 2 * time.Second}
		if resp, err := hc.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				st.DaemonOK = true
				st.DaemonAddr = m.Addr
			}
		}
	}

	if st.SSHOK && !st.DaemonOK {
		st.Error = "daemon not healthy"
	}
	st.LatencyMS = time.Since(start).Milliseconds()
	return st
}
