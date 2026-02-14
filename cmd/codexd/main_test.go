package main

import "testing"

func TestDefaultCodexdConfigPath(t *testing.T) {
	if defaultCodexdConfigPath != "~/.config/codexd/config.yaml" {
		t.Fatalf("defaultCodexdConfigPath = %q", defaultCodexdConfigPath)
	}
}
