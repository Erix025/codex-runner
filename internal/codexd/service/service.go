package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"codex-runner/internal/codexd/config"
	"codex-runner/internal/shared/id"
	"codex-runner/internal/shared/jsonutil"
	"codex-runner/internal/shared/tail"
)

const Version = "0.1.0"

type Service struct {
	cfg config.Config

	mu sync.Mutex
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /v1/exec", s.auth(s.handleExecStart))
	mux.HandleFunc("GET /v1/exec/{id}", s.auth(s.handleExecGet))
	mux.HandleFunc("GET /v1/exec/{id}/logs", s.auth(s.handleExecLogs))
	mux.HandleFunc("POST /v1/exec/{id}/cancel", s.auth(s.handleExecCancel))
	return mux
}

func (s *Service) auth(next http.HandlerFunc) http.HandlerFunc {
	if s.cfg.AuthToken == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		want := "Bearer " + s.cfg.AuthToken
		if got != want {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = jsonutil.WriteJSON(w, map[string]any{
		"ok":      true,
		"version": Version,
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
		"go":      runtime.Version(),
	})
}

type execRequest struct {
	ProjectID string            `json:"project_id"`
	Ref       string            `json:"ref"`
	Cmd       string            `json:"cmd"`
	Cwd       string            `json:"cwd"`
	Env       map[string]string `json:"env"`
}

type execMeta struct {
	ExecID     string            `json:"exec_id"`
	Status     string            `json:"status"` // running|finished
	ProjectID  string            `json:"project_id,omitempty"`
	Ref        string            `json:"ref,omitempty"`
	Cmd        string            `json:"cmd"`
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env,omitempty"`
	PID        int               `json:"pid,omitempty"`
	StartedAt  string            `json:"started_at,omitempty"`
	FinishedAt string            `json:"finished_at,omitempty"`
	ExitCode   *int              `json:"exit_code,omitempty"`
	Error      string            `json:"error,omitempty"`
}

func (s *Service) handleExecStart(w http.ResponseWriter, r *http.Request) {
	var req execRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	req.Cmd = strings.TrimSpace(req.Cmd)
	if req.Cmd == "" {
		writeErr(w, http.StatusBadRequest, "cmd is required")
		return
	}

	execID, err := id.New()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate exec_id")
		return
	}
	execDir := filepath.Join(s.cfg.DataDir, "exec", execID)
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create exec dir")
		return
	}

	meta := execMeta{
		ExecID:    execID,
		Status:    "running",
		ProjectID: req.ProjectID,
		Ref:       req.Ref,
		Cmd:       req.Cmd,
		Cwd:       req.Cwd,
		Env:       req.Env,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeMeta(execDir, meta); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to write meta")
		return
	}

	// Enforce retention opportunistically.
	_ = s.cleanupRetention()

	go s.runExec(execDir, req, meta)

	_ = jsonutil.WriteJSON(w, map[string]any{
		"exec_id": execID,
		"status":  "running",
	})
}

func (s *Service) runExec(execDir string, req execRequest, meta execMeta) {
	ctx := context.Background()

	workDir, cleanupWorktree, err := s.prepareWorkdir(ctx, execDir, req.ProjectID, req.Ref)
	if err != nil {
		meta.Status = "finished"
		now := time.Now().UTC().Format(time.RFC3339Nano)
		meta.FinishedAt = now
		code := 127
		meta.ExitCode = &code
		meta.Error = err.Error()
		_ = writeMeta(execDir, meta)
		_ = writeExitCode(execDir, code)
		return
	}
	if cleanupWorktree != nil {
		defer cleanupWorktree()
	}

	cwd, err := s.resolveCwd(workDir, req.ProjectID, req.Cwd)
	if err != nil {
		meta.Status = "finished"
		now := time.Now().UTC().Format(time.RFC3339Nano)
		meta.FinishedAt = now
		code := 126
		meta.ExitCode = &code
		meta.Error = err.Error()
		_ = writeMeta(execDir, meta)
		_ = writeExitCode(execDir, code)
		return
	}

	stdoutPath := filepath.Join(execDir, "stdout.log")
	stderrPath := filepath.Join(execDir, "stderr.log")
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer stdoutFile.Close()
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer stderrFile.Close()

	cmd := exec.CommandContext(ctx, "sh", "-lc", req.Cmd)
	cmd.Dir = cwd
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if err := cmd.Start(); err != nil {
		meta.Status = "finished"
		now := time.Now().UTC().Format(time.RFC3339Nano)
		meta.FinishedAt = now
		code := 127
		meta.ExitCode = &code
		meta.Error = err.Error()
		_ = writeMeta(execDir, meta)
		_ = writeExitCode(execDir, code)
		return
	}

	meta.PID = cmd.Process.Pid
	_ = writeMeta(execDir, meta)
	_ = writePID(execDir, meta.PID)

	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
				exitCode = ws.ExitStatus()
			} else {
				exitCode = 1
			}
		} else {
			exitCode = 1
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	meta.Status = "finished"
	meta.FinishedAt = now
	meta.ExitCode = &exitCode
	if err != nil {
		meta.Error = err.Error()
	}
	_ = writeMeta(execDir, meta)
	_ = writeExitCode(execDir, exitCode)
}

func (s *Service) handleExecGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	execDir := filepath.Join(s.cfg.DataDir, "exec", id)
	meta, err := readMeta(execDir)
	if err != nil {
		writeErr(w, http.StatusNotFound, "exec_id not found")
		return
	}
	_ = jsonutil.WriteJSON(w, meta)
}

func (s *Service) handleExecLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	execDir := filepath.Join(s.cfg.DataDir, "exec", id)
	if _, err := os.Stat(execDir); err != nil {
		writeErr(w, http.StatusNotFound, "exec_id not found")
		return
	}
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		stream = "stdout"
	}
	if stream != "stdout" && stream != "stderr" {
		writeErr(w, http.StatusBadRequest, "stream must be stdout or stderr")
		return
	}
	tailStr := r.URL.Query().Get("tail")
	var maxBytes int64 = 2000
	if tailStr != "" {
		if n, err := strconv.ParseInt(tailStr, 10, 64); err == nil && n >= 0 {
			maxBytes = n
		}
	}
	format := r.URL.Query().Get("format") // "" or "jsonl"

	path := filepath.Join(execDir, stream+".log")
	b, err := tail.ReadTailBytes(path, maxBytes)
	if err != nil {
		if os.IsNotExist(err) {
			b = []byte{}
		} else {
			writeErr(w, http.StatusInternalServerError, "failed to read logs")
			return
		}
	}
	if format != "jsonl" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	lines := bytes.Split(b, []byte{'\n'})
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		_ = jsonutil.WriteJSON(w, map[string]any{
			"type":   "log",
			"stream": stream,
			"line":   string(line),
		})
	}
}

func (s *Service) handleExecCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	execDir := filepath.Join(s.cfg.DataDir, "exec", id)
	pid, err := readPID(execDir)
	if err != nil {
		if _, metaErr := readMeta(execDir); metaErr == nil {
			_ = jsonutil.WriteJSON(w, map[string]any{
				"canceled": false,
				"reason":   "not started yet",
			})
			return
		}
		writeErr(w, http.StatusNotFound, "exec_id not found")
		return
	}
	// Best-effort: SIGTERM then SIGKILL after timeout.
	if err := signalProcessGroup(pid, syscall.SIGTERM); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to signal process")
		return
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = jsonutil.WriteJSON(w, map[string]any{"canceled": true})
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = signalProcessGroup(pid, syscall.SIGKILL)
	_ = jsonutil.WriteJSON(w, map[string]any{"canceled": true})
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0: check existence (unix).
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

func signalProcessGroup(pid int, sig syscall.Signal) error {
	// Negative pid means process group on unix.
	return syscall.Kill(-pid, sig)
}

func (s *Service) cleanupRetention() error {
	if s.cfg.RetentionCount <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	execRoot := filepath.Join(s.cfg.DataDir, "exec")
	_ = os.MkdirAll(execRoot, 0o755)
	entries, err := os.ReadDir(execRoot)
	if err != nil {
		return err
	}
	type item struct {
		name string
		mod  time.Time
	}
	var items []item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{name: e.Name(), mod: info.ModTime()})
	}
	if len(items) <= s.cfg.RetentionCount {
		return nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.Before(items[j].mod) })
	toDelete := items[:len(items)-s.cfg.RetentionCount]
	for _, it := range toDelete {
		_ = os.RemoveAll(filepath.Join(execRoot, it.name))
	}
	return nil
}

func (s *Service) prepareWorkdir(ctx context.Context, execDir, projectID, ref string) (string, func(), error) {
	// No project context: run in home dir by default.
	if projectID == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil, err
		}
		return home, nil, nil
	}
	if ref == "" {
		return "", nil, errors.New("ref is required when project_id is set")
	}
	var proj *config.Project
	for i := range s.cfg.Projects {
		if s.cfg.Projects[i].ID == projectID {
			proj = &s.cfg.Projects[i]
			break
		}
	}
	if proj == nil {
		return "", nil, fmt.Errorf("unknown project_id: %s", projectID)
	}
	mirrorDir := proj.MirrorDir
	if mirrorDir == "" {
		mirrorDir = filepath.Join(s.cfg.DataDir, "mirrors", projectID+".git")
	}
	if err := os.MkdirAll(filepath.Dir(mirrorDir), 0o755); err != nil {
		return "", nil, err
	}

	if _, err := os.Stat(mirrorDir); os.IsNotExist(err) {
		if err := runGit(ctx, "", "clone", "--mirror", proj.RepoURL, mirrorDir); err != nil {
			return "", nil, fmt.Errorf("git clone --mirror failed: %w", err)
		}
	} else {
		if err := runGit(ctx, mirrorDir, "fetch", "--prune"); err != nil {
			return "", nil, fmt.Errorf("git fetch failed: %w", err)
		}
	}

	commit, err := gitRevParse(ctx, mirrorDir, ref)
	if err != nil {
		return "", nil, err
	}
	workdir := filepath.Join(execDir, "workdir")
	if err := runGit(ctx, mirrorDir, "worktree", "add", "--force", workdir, commit); err != nil {
		return "", nil, fmt.Errorf("git worktree add failed: %w", err)
	}
	cleanup := func() {
		_ = runGit(context.Background(), mirrorDir, "worktree", "remove", "--force", workdir)
	}
	return workdir, cleanup, nil
}

func gitRevParse(ctx context.Context, mirrorDir, ref string) (string, error) {
	out, err := runGitOutput(ctx, mirrorDir, "rev-parse", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func runGit(ctx context.Context, mirrorDir string, args ...string) error {
	_, err := runGitOutput(ctx, mirrorDir, args...)
	return err
}

func runGitOutput(ctx context.Context, mirrorDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if mirrorDir != "" {
		// Mirror is a bare repo; -C works fine.
		cmd.Args = append([]string{"git", "-C", mirrorDir}, args...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return string(out), nil
}

func (s *Service) resolveCwd(workDir, projectID, cwd string) (string, error) {
	if cwd == "" {
		return workDir, nil
	}
	if filepath.IsAbs(cwd) {
		// If project context is set, restrict to within workDir.
		if projectID != "" {
			if !isWithin(workDir, cwd) {
				return "", errors.New("cwd must be within project workdir")
			}
			return cwd, nil
		}
		// No project context: restrict to allowed roots if configured, otherwise allow home + data dir.
		roots := append([]string{}, s.cfg.AllowedCwdRoots...)
		home, _ := os.UserHomeDir()
		if home != "" {
			roots = append(roots, home)
		}
		roots = append(roots, s.cfg.DataDir)
		for _, root := range roots {
			if isWithin(root, cwd) {
				return cwd, nil
			}
		}
		return "", errors.New("cwd not allowed")
	}
	// Relative path.
	base := workDir
	if projectID == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = home
	}
	abs := filepath.Join(base, cwd)
	return abs, nil
}

func isWithin(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if root == p {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func metaPath(execDir string) string { return filepath.Join(execDir, "meta.json") }

func writeMeta(execDir string, meta execMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(execDir), b, 0o644)
}

func readMeta(execDir string) (execMeta, error) {
	b, err := os.ReadFile(metaPath(execDir))
	if err != nil {
		return execMeta{}, err
	}
	var meta execMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return execMeta{}, err
	}
	return meta, nil
}

func writeExitCode(execDir string, code int) error {
	return os.WriteFile(filepath.Join(execDir, "exit_code"), []byte(strconv.Itoa(code)), 0o644)
}

func writePID(execDir string, pid int) error {
	return os.WriteFile(filepath.Join(execDir, "pid"), []byte(strconv.Itoa(pid)), 0o644)
}

func readPID(execDir string) (int, error) {
	b, err := os.ReadFile(filepath.Join(execDir, "pid"))
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, err
	}
	return n, nil
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = jsonutil.WriteJSON(w, map[string]any{
		"error": msg,
	})
}
