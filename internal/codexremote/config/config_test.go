package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDefaultConfigCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nested", "config.yaml")
	created, resolved, err := EnsureDefaultConfig(path)
	if err != nil {
		t.Fatalf("EnsureDefaultConfig() error = %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true")
	}
	if resolved != path {
		t.Fatalf("resolved = %q, want %q", resolved, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(b), "machines:") {
		t.Fatalf("default template missing machines")
	}
}

func TestEnsureDefaultConfigDoesNotOverwrite(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("machines: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, _, err := EnsureDefaultConfig(path)
	if err != nil {
		t.Fatalf("EnsureDefaultConfig() error = %v", err)
	}
	if created {
		t.Fatalf("created = true, want false")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "machines: []\n" {
		t.Fatalf("existing config was overwritten")
	}
}

func TestLoadAfterEnsureDefaultConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	_, _, err := EnsureDefaultConfig(path)
	if err != nil {
		t.Fatalf("EnsureDefaultConfig() error = %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Machines) == 0 {
		t.Fatalf("len(cfg.Machines) = 0, want >= 1")
	}
}
