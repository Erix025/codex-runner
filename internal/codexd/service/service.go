package service

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	mux.HandleFunc("POST /v1/exec/run", s.auth(s.handleExecRun))
	mux.HandleFunc("GET /v1/exec/{id}", s.auth(s.handleExecGet))
	mux.HandleFunc("GET /v1/exec/{id}/logs", s.auth(s.handleExecLogs))
	mux.HandleFunc("POST /v1/exec/{id}/cancel", s.auth(s.handleExecCancel))
	mux.HandleFunc("POST /v1/file/write", s.auth(s.handleFileWrite))
	mux.HandleFunc("POST /v1/file/read", s.auth(s.handleFileRead))
	mux.HandleFunc("POST /v1/sync/upload", s.auth(s.handleSyncUpload))
	mux.HandleFunc("POST /v1/sync/download", s.auth(s.handleSyncDownload))
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
	Artifacts  json.RawMessage   `json:"artifacts,omitempty"`
	Warn       string            `json:"warning,omitempty"`
}

func (s *Service) handleExecStart(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeExecRequest(w, r)
	if !ok {
		return
	}
	execID, execDir, meta, err := s.initExec(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	go s.runExec(execDir, req, meta)

	_ = jsonutil.WriteJSON(w, map[string]any{
		"exec_id": execID,
		"status":  "running",
	})
}

func (s *Service) handleExecRun(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeExecRequest(w, r)
	if !ok {
		return
	}
	execID, execDir, meta, err := s.initExec(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	ew, err := newEventWriter(w)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	if err := ew.Write(map[string]any{
		"type":       "started",
		"exec_id":    execID,
		"status":     "running",
		"started_at": meta.StartedAt,
	}); err != nil {
		return
	}

	s.runExecStreaming(r.Context(), execDir, req, meta, ew)
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
	configureCmd(cmd)
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
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
			if exitCode < 0 {
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
	if artifacts, warn := collectArtifacts(execDir, cwd); len(artifacts) > 0 {
		meta.Artifacts = artifacts
		meta.Warn = warn
	} else if warn != "" {
		meta.Warn = warn
	}
	_ = writeMeta(execDir, meta)
	_ = writeExitCode(execDir, exitCode)
}

func (s *Service) runExecStreaming(ctx context.Context, execDir string, req execRequest, meta execMeta, ew *eventWriter) {
	workDir, cleanupWorktree, err := s.prepareWorkdir(ctx, execDir, req.ProjectID, req.Ref)
	if err != nil {
		finished := s.finalizeMeta(execDir, meta, 127, err)
		_ = ew.Write(finishedEvent(finished))
		return
	}
	if cleanupWorktree != nil {
		defer cleanupWorktree()
	}

	cwd, err := s.resolveCwd(workDir, req.ProjectID, req.Cwd)
	if err != nil {
		finished := s.finalizeMeta(execDir, meta, 126, err)
		_ = ew.Write(finishedEvent(finished))
		return
	}

	stdoutPath := filepath.Join(execDir, "stdout.log")
	stderrPath := filepath.Join(execDir, "stderr.log")
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		finished := s.finalizeMeta(execDir, meta, 127, err)
		_ = ew.Write(finishedEvent(finished))
		return
	}
	defer stdoutFile.Close()
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		finished := s.finalizeMeta(execDir, meta, 127, err)
		_ = ew.Write(finishedEvent(finished))
		return
	}
	defer stderrFile.Close()

	cmd := exec.CommandContext(ctx, "sh", "-lc", req.Cmd)
	cmd.Dir = cwd
	configureCmd(cmd)
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		finished := s.finalizeMeta(execDir, meta, 127, err)
		_ = ew.Write(finishedEvent(finished))
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		finished := s.finalizeMeta(execDir, meta, 127, err)
		_ = ew.Write(finishedEvent(finished))
		return
	}

	if err := cmd.Start(); err != nil {
		finished := s.finalizeMeta(execDir, meta, 127, err)
		_ = ew.Write(finishedEvent(finished))
		return
	}

	meta.PID = cmd.Process.Pid
	_ = writeMeta(execDir, meta)
	_ = writePID(execDir, meta.PID)

	streamErrs := make(chan error, 2)
	go func() {
		streamErrs <- streamToFileAndEvents(stdoutPipe, stdoutFile, "stdout", ew)
	}()
	go func() {
		streamErrs <- streamToFileAndEvents(stderrPipe, stderrFile, "stderr", ew)
	}()

	waitErr := cmd.Wait()
	firstStreamErr := <-streamErrs
	secondStreamErr := <-streamErrs
	streamErr := firstNonNil(firstStreamErr, secondStreamErr)

	exitCode := 0
	if waitErr != nil {
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
			if exitCode < 0 {
				exitCode = 1
			}
		} else {
			exitCode = 1
		}
	}

	finalErr := waitErr
	if finalErr == nil {
		finalErr = streamErr
	}
	finished := s.finalizeMeta(execDir, meta, exitCode, finalErr)
	_ = ew.Write(finishedEvent(finished))
}

func decodeExecRequest(w http.ResponseWriter, r *http.Request) (execRequest, bool) {
	var req execRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return execRequest{}, false
	}
	req.Cmd = strings.TrimSpace(req.Cmd)
	if req.Cmd == "" {
		writeErr(w, http.StatusBadRequest, "cmd is required")
		return execRequest{}, false
	}
	return req, true
}

func (s *Service) initExec(req execRequest) (string, string, execMeta, error) {
	execID, err := id.New()
	if err != nil {
		return "", "", execMeta{}, errors.New("failed to generate exec_id")
	}
	execDir := filepath.Join(s.cfg.DataDir, "exec", execID)
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		return "", "", execMeta{}, errors.New("failed to create exec dir")
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
		return "", "", execMeta{}, errors.New("failed to write meta")
	}
	_ = s.cleanupRetention()
	return execID, execDir, meta, nil
}

func (s *Service) finalizeMeta(execDir string, meta execMeta, exitCode int, err error) execMeta {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	meta.Status = "finished"
	meta.FinishedAt = now
	meta.ExitCode = &exitCode
	if err != nil {
		meta.Error = err.Error()
	}
	_ = writeMeta(execDir, meta)
	_ = writeExitCode(execDir, exitCode)
	return meta
}

func finishedEvent(meta execMeta) map[string]any {
	out := map[string]any{
		"type":        "finished",
		"exec_id":     meta.ExecID,
		"status":      meta.Status,
		"finished_at": meta.FinishedAt,
		"exit_code":   0,
	}
	if meta.ExitCode != nil {
		out["exit_code"] = *meta.ExitCode
	}
	if meta.Error != "" {
		out["error"] = meta.Error
	}
	return out
}

func streamToFileAndEvents(src io.Reader, dst *os.File, stream string, ew *eventWriter) error {
	reader := bufio.NewReader(src)
	for {
		chunk, err := reader.ReadString('\n')
		if len(chunk) > 0 {
			if _, werr := dst.WriteString(chunk); werr != nil {
				return werr
			}
			line := strings.TrimSuffix(chunk, "\n")
			line = strings.TrimSuffix(line, "\r")
			if err := ew.Write(map[string]any{
				"type":   "log",
				"stream": stream,
				"line":   line,
			}); err != nil {
				return err
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

type eventWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func newEventWriter(w http.ResponseWriter) (*eventWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("response writer is not flushable")
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	return &eventWriter{
		w:       w,
		flusher: flusher,
	}, nil
}

func (ew *eventWriter) Write(v any) error {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	if err := jsonutil.WriteJSON(ew.w, v); err != nil {
		return err
	}
	ew.flusher.Flush()
	return nil
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
	tailLinesStr := r.URL.Query().Get("tail_lines")
	var maxBytes int64 = 2000
	var maxLines int
	if tailStr != "" {
		if n, err := strconv.ParseInt(tailStr, 10, 64); err == nil && n >= 0 {
			maxBytes = n
		}
	}
	if tailLinesStr != "" {
		n, err := strconv.Atoi(tailLinesStr)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "tail_lines must be >= 0")
			return
		}
		maxLines = n
	}
	sinceFilter, err := parseRFC3339(r.URL.Query().Get("since"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "since must be RFC3339")
		return
	}
	untilFilter, err := parseRFC3339(r.URL.Query().Get("until"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "until must be RFC3339")
		return
	}
	format := r.URL.Query().Get("format") // "" or "jsonl"

	path := filepath.Join(execDir, stream+".log")
	var b []byte
	if maxLines > 0 {
		b, err = tail.ReadTailLines(path, maxLines)
	} else if tailStr != "" {
		b, err = tail.ReadTailBytes(path, maxBytes)
	} else if sinceFilter != nil || untilFilter != nil {
		b, err = tail.ReadAll(path)
	} else {
		b, err = tail.ReadTailBytes(path, maxBytes)
	}
	if err != nil {
		if os.IsNotExist(err) {
			b = []byte{}
		} else {
			writeErr(w, http.StatusInternalServerError, "failed to read logs")
			return
		}
	}
	filtered := filterLogLinesByTime(bytes.Split(b, []byte{'\n'}), sinceFilter, untilFilter)
	b = bytes.Join(filtered, []byte{'\n'})
	if format != "jsonl" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	for _, line := range filtered {
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
	if err := gracefulStopExec(pid); err != nil {
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
	_ = forceStopExec(pid)
	_ = jsonutil.WriteJSON(w, map[string]any{"canceled": true})
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

func parseRFC3339(v string) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func filterLogLinesByTime(lines [][]byte, since, until *time.Time) [][]byte {
	if since == nil && until == nil {
		return lines
	}
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		ts, ok := extractLogTime(line)
		if !ok {
			out = append(out, line)
			continue
		}
		if since != nil && ts.Before(*since) {
			continue
		}
		if until != nil && ts.After(*until) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func extractLogTime(line []byte) (time.Time, bool) {
	var obj map[string]any
	if err := json.Unmarshal(line, &obj); err != nil {
		return time.Time{}, false
	}
	for _, key := range []string{"ts", "time", "timestamp", "@timestamp"} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func collectArtifacts(execDir, cwd string) (json.RawMessage, string) {
	candidates := []string{
		filepath.Join(cwd, ".codex", "artifacts.json"),
		filepath.Join(execDir, "artifacts.json"),
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if !json.Valid(b) {
			return nil, "artifacts.json is not valid JSON"
		}
		var obj map[string]any
		if err := json.Unmarshal(b, &obj); err != nil {
			return nil, "artifacts.json decode failed"
		}
		arts, ok := obj["artifacts"].([]any)
		if !ok {
			return nil, "artifacts.json missing artifacts array"
		}
		if len(arts) == 0 {
			return nil, ""
		}
		out, err := json.Marshal(arts)
		if err != nil {
			return nil, "artifacts.json marshal failed"
		}
		return json.RawMessage(out), ""
	}
	return nil, ""
}

type fileWriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"` // base64
	Mode    int    `json:"mode,omitempty"`
	MkdirP  bool   `json:"mkdir_p,omitempty"`
}

type fileReadRequest struct {
	Path string `json:"path"`
}

func (s *Service) isPathAllowed(p string) bool {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return false
	}
	roots := append([]string{}, s.cfg.AllowedCwdRoots...)
	home, _ := os.UserHomeDir()
	if home != "" {
		roots = append(roots, home)
	}
	roots = append(roots, s.cfg.DataDir)
	// Also allow /tmp
	roots = append(roots, "/tmp")
	for _, root := range roots {
		if isWithin(root, p) {
			return true
		}
	}
	return false
}

func (s *Service) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	var req fileWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if !filepath.IsAbs(req.Path) {
		writeErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	if !s.isPathAllowed(req.Path) {
		writeErr(w, http.StatusForbidden, "path not allowed")
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid base64 content")
		return
	}
	if s.cfg.MaxFileSize > 0 && int64(len(data)) > s.cfg.MaxFileSize {
		writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	mode := os.FileMode(0o644)
	if req.Mode > 0 {
		mode = os.FileMode(req.Mode)
	}
	if req.MkdirP {
		if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create directory: "+err.Error())
			return
		}
	}
	// Atomic write: temp file + rename
	tmpFile, err := os.CreateTemp(filepath.Dir(req.Path), ".codexd-write-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create temp file: "+err.Error())
		return
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath) // cleanup on failure
	}()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		writeErr(w, http.StatusInternalServerError, "failed to write: "+err.Error())
		return
	}
	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		writeErr(w, http.StatusInternalServerError, "failed to set mode: "+err.Error())
		return
	}
	if err := tmpFile.Close(); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to close: "+err.Error())
		return
	}
	if err := os.Rename(tmpPath, req.Path); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to rename: "+err.Error())
		return
	}
	_ = jsonutil.WriteJSON(w, map[string]any{
		"ok":            true,
		"path":          req.Path,
		"bytes_written": len(data),
	})
}

func (s *Service) handleFileRead(w http.ResponseWriter, r *http.Request) {
	var req fileReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if !filepath.IsAbs(req.Path) {
		writeErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	if !s.isPathAllowed(req.Path) {
		writeErr(w, http.StatusForbidden, "path not allowed")
		return
	}
	info, err := os.Stat(req.Path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "file not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed to stat: "+err.Error())
		return
	}
	if info.IsDir() {
		writeErr(w, http.StatusBadRequest, "path is a directory")
		return
	}
	if s.cfg.MaxFileSize > 0 && info.Size() > s.cfg.MaxFileSize {
		writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	data, err := os.ReadFile(req.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read: "+err.Error())
		return
	}
	_ = jsonutil.WriteJSON(w, map[string]any{
		"ok":      true,
		"path":    req.Path,
		"content": base64.StdEncoding.EncodeToString(data),
		"size":    len(data),
	})
}

func (s *Service) handleSyncUpload(w http.ResponseWriter, r *http.Request) {
	dst := r.URL.Query().Get("dst")
	if dst == "" {
		writeErr(w, http.StatusBadRequest, "dst query parameter is required")
		return
	}
	if !filepath.IsAbs(dst) {
		writeErr(w, http.StatusBadRequest, "dst must be absolute")
		return
	}
	if !s.isPathAllowed(dst) {
		writeErr(w, http.StatusForbidden, "dst path not allowed")
		return
	}
	mkdirP := r.URL.Query().Get("mkdir_p") == "true"
	if mkdirP {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create directory: "+err.Error())
			return
		}
	}

	gr, err := gzip.NewReader(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid gzip: "+err.Error())
		return
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	filesWritten := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid tar: "+err.Error())
			return
		}

		target := filepath.Join(dst, hdr.Name)
		// Security: prevent path traversal
		if !isWithin(dst, target) {
			writeErr(w, http.StatusBadRequest, "path traversal detected: "+hdr.Name)
			return
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				writeErr(w, http.StatusInternalServerError, "failed to create dir: "+err.Error())
				return
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				writeErr(w, http.StatusInternalServerError, "mkdir failed: "+err.Error())
				return
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "failed to create file: "+err.Error())
				return
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				writeErr(w, http.StatusInternalServerError, "failed to write file: "+err.Error())
				return
			}
			_ = f.Close()
			filesWritten++
		case tar.TypeSymlink:
			// Validate symlink target is within dst
			linkTarget := hdr.Linkname
			if filepath.IsAbs(linkTarget) {
				if !isWithin(dst, linkTarget) {
					writeErr(w, http.StatusBadRequest, "symlink traversal: "+hdr.Linkname)
					return
				}
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				writeErr(w, http.StatusInternalServerError, "failed to create symlink: "+err.Error())
				return
			}
			filesWritten++
		}
	}

	_ = jsonutil.WriteJSON(w, map[string]any{
		"ok":            true,
		"path":          dst,
		"files_written": filesWritten,
	})
}

func (s *Service) handleSyncDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string   `json:"path"`
		Excludes []string `json:"excludes,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if !filepath.IsAbs(req.Path) {
		writeErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	if !s.isPathAllowed(req.Path) {
		writeErr(w, http.StatusForbidden, "path not allowed")
		return
	}

	info, err := os.Stat(req.Path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "path not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed to stat: "+err.Error())
		return
	}
	if !info.IsDir() {
		writeErr(w, http.StatusBadRequest, "path must be a directory")
		return
	}

	w.Header().Set("Content-Type", "application/x-tar+gzip")
	gw := gzip.NewWriter(w)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	_ = filepath.Walk(req.Path, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}

		rel, err := filepath.Rel(req.Path, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}

		// Check excludes (simple name-based matching)
		for _, pattern := range req.Excludes {
			if matched, _ := filepath.Match(pattern, fi.Name()); matched {
				if fi.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return nil
		}
		hdr.Name = rel

		// Handle symlinks
		if fi.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return nil
			}
			hdr.Linkname = link
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if fi.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			_, _ = io.Copy(tw, f)
		}

		return nil
	})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = jsonutil.WriteJSON(w, map[string]any{
		"error": msg,
	})
}
