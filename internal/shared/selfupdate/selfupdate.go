package selfupdate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRepoOwner = "Erix025"
	defaultRepoName  = "codex-runner"
	defaultGitHubAPI = "https://api.github.com"
)

var ErrUnsupportedPlatform = errors.New("self update is not supported on this platform yet")
var osExecutable = os.Executable

type Updater struct {
	BinaryName     string
	CurrentVersion string
	RepoOwner      string
	RepoName       string
	BaseAPI        string
	HTTPClient     *http.Client

	fetchLatestFn func(ctx context.Context) (latestRelease, error)
	downloadFn    func(ctx context.Context, url string) ([]byte, error)
}

type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	Comparable      bool
	UpdateAvailable bool
	AssetName       string
}

type latestRelease struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (u *Updater) Check(ctx context.Context, goos, goarch string) (CheckResult, error) {
	if err := ensurePlatformSupported(goos); err != nil {
		return CheckResult{}, err
	}
	rel, err := u.fetchLatest(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	assetName := binaryAssetName(u.BinaryName, goos, goarch)
	if _, ok := findAsset(rel.Assets, assetName); !ok {
		return CheckResult{}, fmt.Errorf("release asset %q not found", assetName)
	}

	out := CheckResult{
		CurrentVersion: u.CurrentVersion,
		LatestVersion:  rel.TagName,
		AssetName:      assetName,
	}
	cur, okCur := parseSemver(u.CurrentVersion)
	lat, okLat := parseSemver(rel.TagName)
	if okCur && okLat {
		out.Comparable = true
		out.UpdateAvailable = compareSemver(cur, lat) < 0
	}
	return out, nil
}

func (u *Updater) Update(ctx context.Context, goos, goarch string) (string, error) {
	if err := ensurePlatformSupported(goos); err != nil {
		return "", err
	}
	rel, err := u.fetchLatest(ctx)
	if err != nil {
		return "", err
	}

	binAssetName := binaryAssetName(u.BinaryName, goos, goarch)
	binAsset, ok := findAsset(rel.Assets, binAssetName)
	if !ok {
		return "", fmt.Errorf("release asset %q not found", binAssetName)
	}
	sumsAsset, ok := findAsset(rel.Assets, "SHA256SUMS")
	if !ok {
		return "", errors.New("release asset \"SHA256SUMS\" not found")
	}

	binData, err := u.download(ctx, binAsset.BrowserDownloadURL)
	if err != nil {
		return "", err
	}
	sumsData, err := u.download(ctx, sumsAsset.BrowserDownloadURL)
	if err != nil {
		return "", err
	}

	expectedPath := distChecksumPath(u.BinaryName, goos, goarch)
	expectedSum, err := checksumForPath(sumsData, expectedPath)
	if err != nil {
		return "", err
	}
	actual := sha256.Sum256(binData)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(expectedSum, actualHex) {
		return "", fmt.Errorf("checksum mismatch for %s: expected %s got %s", binAssetName, expectedSum, actualHex)
	}

	target, err := osExecutable()
	if err != nil {
		return "", err
	}
	if err := replaceExecutable(target, binData); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func (u *Updater) fetchLatest(ctx context.Context) (latestRelease, error) {
	if u.fetchLatestFn != nil {
		return u.fetchLatestFn(ctx)
	}
	owner := u.RepoOwner
	if owner == "" {
		owner = defaultRepoOwner
	}
	repo := u.RepoName
	if repo == "" {
		repo = defaultRepoName
	}
	api := strings.TrimRight(u.BaseAPI, "/")
	if api == "" {
		api = defaultGitHubAPI
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", api, owner, repo)

	hc := u.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return latestRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := hc.Do(req)
	if err != nil {
		return latestRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return latestRelease{}, fmt.Errorf("github latest release request failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	var rel latestRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return latestRelease{}, err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return latestRelease{}, errors.New("latest release is missing tag_name")
	}
	return rel, nil
}

func (u *Updater) download(ctx context.Context, url string) ([]byte, error) {
	if u.downloadFn != nil {
		return u.downloadFn(ctx, url)
	}
	hc := u.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download failed: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(resp.Body)
}

func ensurePlatformSupported(goos string) error {
	switch goos {
	case "linux", "darwin":
		return nil
	case "":
		return ensurePlatformSupported(runtime.GOOS)
	default:
		return ErrUnsupportedPlatform
	}
}

func binaryAssetName(binary, goos, goarch string) string {
	name := fmt.Sprintf("%s-%s-%s", binary, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func distChecksumPath(binary, goos, goarch string) string {
	name := binary
	if goos == "windows" {
		name += ".exe"
	}
	return fmt.Sprintf("./%s-%s/%s", goos, goarch, name)
}

func findAsset(assets []asset, name string) (asset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return asset{}, false
}

func checksumForPath(sums []byte, path string) (string, error) {
	s := bufio.NewScanner(bytes.NewReader(sums))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if parts[1] == path {
			return strings.ToLower(parts[0]), nil
		}
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksum for %q not found in SHA256SUMS", path)
}

func replaceExecutable(target string, newBin []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".new-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(newBin); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpPath, target)
}

type semver struct {
	major int
	minor int
	patch int
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, false
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, false
	}
	pat, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, false
	}
	return semver{major: maj, minor: min, patch: pat}, true
}

func compareSemver(a, b semver) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	return 0
}
