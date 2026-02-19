package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"codex-runner/internal/codexremote/client"
	"codex-runner/internal/shared/jsonutil"
)

func execWatch(args []string) {
	fs := flag.NewFlagSet("exec watch", flag.ExitOnError)
	cfgPath := configFlag(fs)
	machineName := fs.String("machine", "", "machine name")
	execID := fs.String("id", "", "exec id")
	stream := fs.String("stream", "both", "stdout|stderr|both")
	poll := fs.Duration("poll", time.Second, "poll interval")
	tail := fs.Int64("tail", 2000, "tail bytes fetched each poll")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *machineName == "" || *execID == "" {
		fmt.Fprintln(os.Stderr, "--machine and --id are required")
		os.Exit(2)
	}
	if *stream != "stdout" && *stream != "stderr" && *stream != "both" {
		fmt.Fprintln(os.Stderr, "--stream must be stdout|stderr|both")
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
	cl, closer, _, err := connectClientForExec(*m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer()
	}

	streams := []string{"stdout", "stderr"}
	if *stream == "stdout" {
		streams = []string{"stdout"}
	}
	if *stream == "stderr" {
		streams = []string{"stderr"}
	}
	seen := map[string][]string{}
	start := time.Now()
	var lastMeta map[string]any

	for {
		for _, s := range streams {
			lines, err := fetchLogLines(cl, *execID, s, *tail)
			if err != nil && !isRetryableExecErr(err) {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err != nil {
				continue
			}
			newLines := deltaLines(seen[s], lines)
			for _, line := range newLines {
				ev := map[string]any{"type": "log", "stream": s, "line": line}
				_ = jsonutil.WriteJSON(os.Stdout, ev)
			}
			seen[s] = lines
		}

		meta, err := fetchExecMeta(cl, *execID)
		if err != nil {
			if isRetryableExecErr(err) {
				time.Sleep(*poll)
				continue
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lastMeta = meta
		if status, _ := meta["status"].(string); status == "finished" {
			break
		}
		time.Sleep(*poll)
	}

	exitCode := 0
	if v, ok := lastMeta["exit_code"].(float64); ok {
		exitCode = int(v)
	}
	summary := map[string]any{
		"type":            "summary",
		"exec_id":         *execID,
		"status":          lastMeta["status"],
		"exit_code":       exitCode,
		"duration_ms":     time.Since(start).Milliseconds(),
		"stdout_log_path": fmt.Sprintf("exec/%s/stdout.log", *execID),
		"stderr_log_path": fmt.Sprintf("exec/%s/stderr.log", *execID),
	}
	if arts, ok := lastMeta["artifacts"]; ok {
		summary["artifacts"] = arts
	}
	_ = jsonutil.WriteJSON(os.Stdout, summary)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func fetchExecMeta(cl *client.Client, execID string) (map[string]any, error) {
	var out map[string]any
	err := withRetry(3, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		b, err := cl.ExecGet(ctx, execID)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, &out)
	})
	return out, err
}

func fetchLogLines(cl *client.Client, execID string, stream string, tail int64) ([]string, error) {
	var raw []byte
	err := withRetry(3, func() error {
		var buf bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := cl.ExecLogs(ctx, execID, stream, tail, "jsonl", &buf); err != nil {
			return err
		}
		raw = append(raw[:0], buf.Bytes()...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return parseNDJSONLogLines(raw), nil
}

func parseNDJSONLogLines(b []byte) []string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		v, ok := obj["line"].(string)
		if !ok {
			continue
		}
		out = append(out, v)
	}
	return out
}

func deltaLines(prev, curr []string) []string {
	if len(prev) == 0 {
		return curr
	}
	max := len(prev)
	if len(curr) < max {
		max = len(curr)
	}
	overlap := 0
	for k := max; k > 0; k-- {
		if sameStrings(prev[len(prev)-k:], curr[:k]) {
			overlap = k
			break
		}
	}
	return curr[overlap:]
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
