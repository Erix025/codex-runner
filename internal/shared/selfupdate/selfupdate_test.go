package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	u := Updater{
		BinaryName:     "codex-remote",
		CurrentVersion: "v1.2.2",
		fetchLatestFn: func(context.Context) (latestRelease, error) {
			return latestRelease{
				TagName: "v1.2.3",
				Assets: []asset{
					{Name: "codex-remote-darwin-arm64", BrowserDownloadURL: "bin"},
					{Name: "SHA256SUMS", BrowserDownloadURL: "sums"},
				},
			}, nil
		},
	}
	got, err := u.Check(context.Background(), "darwin", "arm64")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if got.LatestVersion != "v1.2.3" {
		t.Fatalf("LatestVersion = %q", got.LatestVersion)
	}
	if !got.Comparable {
		t.Fatalf("Comparable = false, want true")
	}
	if !got.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
}

func TestCheckUnsupportedPlatform(t *testing.T) {
	u := Updater{BinaryName: "codex-remote"}
	_, err := u.Check(context.Background(), "windows", "amd64")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("Check() err = %v, want unsupported", err)
	}
}

func TestChecksumForPath(t *testing.T) {
	sums := []byte("abc123  ./darwin-arm64/codex-remote\n")
	got, err := checksumForPath(sums, "./darwin-arm64/codex-remote")
	if err != nil {
		t.Fatalf("checksumForPath() error = %v", err)
	}
	if got != "abc123" {
		t.Fatalf("checksumForPath() = %q", got)
	}
}

func TestUpdateReplacesExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	bin := []byte("new-binary-data")
	sum := sha256.Sum256(bin)
	sums := []byte(fmt.Sprintf("%s  ./darwin-arm64/codex-remote\n", hex.EncodeToString(sum[:])))

	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "codex-remote")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	u := Updater{
		BinaryName:     "codex-remote",
		CurrentVersion: "v1.0.0",
		fetchLatestFn: func(context.Context) (latestRelease, error) {
			return latestRelease{
				TagName: "v1.0.1",
				Assets: []asset{
					{Name: "codex-remote-darwin-arm64", BrowserDownloadURL: "bin"},
					{Name: "SHA256SUMS", BrowserDownloadURL: "sums"},
				},
			}, nil
		},
		downloadFn: func(_ context.Context, url string) ([]byte, error) {
			switch url {
			case "bin":
				return bin, nil
			case "sums":
				return sums, nil
			default:
				return nil, fmt.Errorf("unexpected url %q", url)
			}
		},
	}

	origExecutable := osExecutable
	osExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { osExecutable = origExecutable })

	latest, err := u.Update(context.Background(), "darwin", "arm64")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if latest != "v1.0.1" {
		t.Fatalf("latest = %q", latest)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bin) {
		t.Fatalf("executable content mismatch")
	}
}
