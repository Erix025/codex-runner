package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func startExec(t *testing.T, h http.Handler, cmd string) string {
	t.Helper()
	reqBody := map[string]any{"cmd": cmd}
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

func TestExecRunStreamingEcho(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	svc := service.New(cfg)
	h := svc.Handler()

	body := runExec(t, h, map[string]any{
		"cmd": "echo hello",
	})
	events := parseJSONLLines(t, body)
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}
	if events[0]["type"] != "started" {
		t.Fatalf("first event type = %v, want started", events[0]["type"])
	}
	if events[len(events)-1]["type"] != "finished" {
		t.Fatalf("last event type = %v, want finished", events[len(events)-1]["type"])
	}

	foundLog := false
	for _, ev := range events {
		if ev["type"] == "log" && ev["stream"] == "stdout" && ev["line"] == "hello" {
			foundLog = true
			break
		}
	}
	if !foundLog {
		t.Fatalf("expected stdout log hello, got events: %#v", events)
	}
}

func TestExecRunSupportsProjectRefCwdEnv(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	mustInitGitRepo(t, repo)
	mustWriteFile(t, filepath.Join(repo, "sub", "note.txt"), "ok\n")
	mustRun(t, repo, "git", "add", ".")
	mustRun(t, repo, "git", "-c", "commit.gpgsign=false", "commit", "-m", "init")

	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Projects = []config.Project{
		{
			ID:      "p1",
			RepoURL: repo,
		},
	}
	svc := service.New(cfg)
	h := svc.Handler()

	body := runExec(t, h, map[string]any{
		"project_id": "p1",
		"ref":        "HEAD",
		"cwd":        "sub",
		"cmd":        "echo $FOO && pwd",
		"env": map[string]string{
			"FOO": "bar",
		},
	})
	events := parseJSONLLines(t, body)
	var logs []string
	for _, ev := range events {
		if ev["type"] == "log" && ev["stream"] == "stdout" {
			if line, ok := ev["line"].(string); ok {
				logs = append(logs, line)
			}
		}
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "bar") {
		t.Fatalf("expected env output in logs, got: %s", joined)
	}
	if !strings.Contains(joined, "/workdir/sub") {
		t.Fatalf("expected cwd suffix /workdir/sub in logs, got: %s", joined)
	}
}

func TestExecRunCancelOnClientDisconnect(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	svc := service.New(cfg)
	h := svc.Handler()

	reqBody := map[string]any{"cmd": "sleep 30"}
	b, _ := json.Marshal(reqBody)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "http://example/v1/exec/run", bytes.NewReader(b)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if rr.Code/100 != 2 {
		t.Fatalf("exec run status = %d body=%s", rr.Code, rr.Body.String())
	}
	events := parseJSONLLines(t, rr.Body.Bytes())
	execID := eventExecID(events)
	if execID == "" {
		t.Fatalf("missing exec_id in run stream")
	}
	meta := waitFinished(t, h, execID, 5*time.Second)
	if meta["status"] != "finished" {
		t.Fatalf("expected finished, got %v", meta["status"])
	}
}

func TestExecRunPersistsArtifacts(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	svc := service.New(cfg)
	h := svc.Handler()

	body := runExec(t, h, map[string]any{
		"cmd": "echo out && echo err 1>&2",
	})
	events := parseJSONLLines(t, body)
	execID := eventExecID(events)
	if execID == "" {
		t.Fatalf("missing exec_id in stream")
	}

	execDir := filepath.Join(dir, "exec", execID)
	metaPath := filepath.Join(execDir, "meta.json")
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("meta.json missing: %v", err)
	}
	stdoutPath := filepath.Join(execDir, "stdout.log")
	stdout, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("stdout.log missing: %v", err)
	}
	if !strings.Contains(string(stdout), "out") {
		t.Fatalf("stdout.log missing out: %s", string(stdout))
	}
	stderrPath := filepath.Join(execDir, "stderr.log")
	stderr, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("stderr.log missing: %v", err)
	}
	if !strings.Contains(string(stderr), "err") {
		t.Fatalf("stderr.log missing err: %s", string(stderr))
	}
	exitPath := filepath.Join(execDir, "exit_code")
	exitCode, err := os.ReadFile(exitPath)
	if err != nil {
		t.Fatalf("exit_code missing: %v", err)
	}
	if strings.TrimSpace(string(exitCode)) != "0" {
		t.Fatalf("expected exit_code 0, got %s", string(exitCode))
	}
}

func runExec(t *testing.T, h http.Handler, reqBody map[string]any) []byte {
	t.Helper()
	b, _ := json.Marshal(reqBody)
	return do(t, h, "POST", "/v1/exec/run", b)
}

func parseJSONLLines(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(body), []byte{'\n'})
	var out []map[string]any
	for _, ln := range lines {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(ln, &m); err != nil {
			t.Fatalf("invalid jsonl line %q: %v", string(ln), err)
		}
		out = append(out, m)
	}
	return out
}

func eventExecID(events []map[string]any) string {
	for _, ev := range events {
		if id, ok := ev["exec_id"].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

func mustInitGitRepo(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	mustRun(t, repo, "git", "init")
	mustRun(t, repo, "git", "config", "user.email", "tester@example.com")
	mustRun(t, repo, "git", "config", "user.name", "Tester")
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, string(out))
	}
}
