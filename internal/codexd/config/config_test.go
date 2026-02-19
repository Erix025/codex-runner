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
	if !strings.Contains(string(b), "listen:") {
		t.Fatalf("default template missing listen")
	}
}

func TestEnsureDefaultConfigDoesNotOverwrite(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:7337\n"), 0o644); err != nil {
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
	if string(b) != "listen: 127.0.0.1:7337\n" {
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
	if cfg.Listen == "" {
		t.Fatalf("cfg.Listen is empty")
	}
}
