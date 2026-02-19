package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-runner/internal/codexremote/machcheck"
)

func TestMachineListSummary(t *testing.T) {
	statuses := []machcheck.Status{
		{Name: "a", SSHOK: true, DaemonOK: true},
		{Name: "b", SSHOK: true, DaemonOK: false},
		{Name: "c", SSHOK: false, DaemonOK: false},
	}

	s := machineListSummary(statuses)
	if s.Total != 3 {
		t.Fatalf("Total = %d, want 3", s.Total)
	}
	if s.SSHOK != 2 {
		t.Fatalf("SSHOK = %d, want 2", s.SSHOK)
	}
	if s.DaemonOK != 1 {
		t.Fatalf("DaemonOK = %d, want 1", s.DaemonOK)
	}
	if s.Failed != 2 {
		t.Fatalf("Failed = %d, want 2", s.Failed)
	}
}

func TestWriteMachineListTable(t *testing.T) {
	statuses := []machcheck.Status{
		{Name: "gpu1", SSHOK: true, DaemonOK: true, LatencyMS: 11},
		{Name: "gpu2", SSHOK: false, DaemonOK: false, LatencyMS: 54, Error: "ssh not reachable"},
	}

	var buf bytes.Buffer
	if err := writeMachineListTable(&buf, statuses); err != nil {
		t.Fatalf("writeMachineListTable() error = %v", err)
	}
	out := buf.String()
	checks := []string{
		"NAME",
		"gpu1",
		"ok",
		"gpu2",
		"down",
		"ssh not reachable",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Fatalf("output missing %q: %s", c, out)
		}
	}
}

func TestLoadConfigBootstrapsDefaultFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if len(cfg.Machines) == 0 {
		t.Fatalf("len(cfg.Machines) = 0, want >= 1")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}
}

func TestDefaultRemoteConfigPath(t *testing.T) {
	if defaultRemoteConfigPath != "~/.config/codex-remote/config.yaml" {
		t.Fatalf("defaultRemoteConfigPath = %q", defaultRemoteConfigPath)
	}
}

func TestParseNDJSONLogLines(t *testing.T) {
	b := []byte(`{"type":"log","stream":"stdout","line":"a"}` + "\n" + `{"type":"log","stream":"stdout","line":"b"}` + "\n")
	got := parseNDJSONLogLines(b)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("parseNDJSONLogLines() = %#v", got)
	}
}

func TestDeltaLines(t *testing.T) {
	prev := []string{"a", "b", "c"}
	curr := []string{"b", "c", "d", "e"}
	got := deltaLines(prev, curr)
	b, _ := json.Marshal(got)
	if string(b) != `["d","e"]` {
		t.Fatalf("deltaLines() = %s, want [\"d\",\"e\"]", b)
	}
}
