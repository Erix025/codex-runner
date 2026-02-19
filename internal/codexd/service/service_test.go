package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codex-runner/internal/codexd/config"
	"codex-runner/internal/codexd/service"
)

func TestExecEchoAndLogsJSONL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.RetentionCount = 50
	svc := service.New(cfg)
	h := svc.Handler()

	execID := startExec(t, h, `echo hello`)
	meta := waitFinished(t, h, execID, 5*time.Second)

	if meta["status"] != "finished" {
		t.Fatalf("expected finished, got %v", meta["status"])
	}
	if meta["exit_code"] == nil {
		t.Fatalf("expected exit_code")
	}

	body := do(t, h, "GET", "/v1/exec/"+execID+"/logs?stream=stdout&tail=2000&format=jsonl", nil)
	lines := bytes.Split(bytes.TrimSpace(body), []byte{'\n'})
	if len(lines) == 0 {
		t.Fatalf("expected jsonl logs")
	}
	found := false
	for _, ln := range lines {
		var ev map[string]any
		if err := json.Unmarshal(ln, &ev); err != nil {
			t.Fatalf("invalid jsonl line: %s", string(ln))
		}
		if ev["type"] == "log" && ev["stream"] == "stdout" && ev["line"] == "hello" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected to find hello in logs, got:\n%s", string(body))
	}
}

func TestExecCancel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	svc := service.New(cfg)
	h := svc.Handler()

	execID := startExec(t, h, `sleep 30`)
	deadline := time.Now().Add(2 * time.Second)
	for {
		b := do(t, h, "POST", "/v1/exec/"+execID+"/cancel", nil)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		if out["canceled"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancel did not take effect in time: %s", string(b))
		}
		time.Sleep(50 * time.Millisecond)
	}
	meta := waitFinished(t, h, execID, 5*time.Second)
	if meta["status"] != "finished" {
		t.Fatalf("expected finished after cancel, got %v", meta["status"])
	}
	if meta["exit_code"] == nil {
		t.Fatalf("expected exit_code after cancel")
	}
}

func TestRetentionCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.RetentionCount = 2
	svc := service.New(cfg)
	h := svc.Handler()

	_ = startExec(t, h, `echo one`)
	_ = startExec(t, h, `echo two`)
	_ = startExec(t, h, `echo three`)

	time.Sleep(200 * time.Millisecond)
	execRoot := filepath.Join(dir, "exec")
	entries, err := os.ReadDir(execRoot)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	if n > 2 {
		t.Fatalf("expected <=2 exec dirs, got %d", n)
	}
}

func TestExecLogsTailLinesAndTimeFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	svc := service.New(cfg)
	h := svc.Handler()

	cmd := `echo '{"time":"2026-01-01T00:00:00Z","msg":"old"}'; echo '{"time":"2026-01-01T01:00:00Z","msg":"new"}'`
	execID := startExec(t, h, cmd)
	_ = waitFinished(t, h, execID, 5*time.Second)

	body := do(t, h, "GET", "/v1/exec/"+execID+"/logs?stream=stdout&tail_lines=10&since=2026-01-01T00:30:00Z&format=jsonl", nil)
	if bytes.Contains(body, []byte(`\"msg\":\"old\"`)) {
		t.Fatalf("expected old log to be filtered out: %s", string(body))
	}
	if !bytes.Contains(body, []byte(`\"msg\":\"new\"`)) {
		t.Fatalf("expected new log in output: %s", string(body))
	}
}

func TestExecCollectArtifacts(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.AllowedCwdRoots = []string{dir}
	svc := service.New(cfg)
	h := svc.Handler()

	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(filepath.Join(work, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmd := `cat > .codex/artifacts.json <<'JSON'
{"version":"1","artifacts":[{"name":"report","type":"file","path":"report.md"}]}
JSON
echo done`
	execID := startExecWithBody(t, h, map[string]any{
		"cmd": cmd,
		"cwd": work,
	})
	meta := waitFinished(t, h, execID, 5*time.Second)
	arts, ok := meta["artifacts"].([]any)
	if !ok || len(arts) != 1 {
		t.Fatalf("expected one artifact, got %#v", meta["artifacts"])
	}
}

func startExec(t *testing.T, h http.Handler, cmd string) string {
	t.Helper()
	reqBody := map[string]any{"cmd": cmd}
	return startExecWithBody(t, h, reqBody)
}

func startExecWithBody(t *testing.T, h http.Handler, reqBody map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(reqBody)
	resp := do(t, h, "POST", "/v1/exec", b)
	var out map[string]any
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("invalid start response: %v", err)
	}
	id, _ := out["exec_id"].(string)
	if id == "" {
		t.Fatalf("missing exec_id")
	}
	return id
}

func waitFinished(t *testing.T, h http.Handler, execID string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b := do(t, h, "GET", "/v1/exec/"+execID, nil)
		var meta map[string]any
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatalf("invalid meta: %v", err)
		}
		if meta["status"] == "finished" {
			return meta
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for finished")
	return nil
}

func do(t *testing.T, h http.Handler, method string, path string, body []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, "http://example"+path, r).WithContext(ctx)
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	res := rr.Result()
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode/100 != 2 {
		t.Fatalf("%s %s => %s: %s", method, path, res.Status, string(b))
	}
	return b
}
