package main

import (
	"bytes"
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
