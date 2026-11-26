// Package chrome finds or installs a Chrome that Stagehand can drive: a Chrome
// already on the machine, or a Chrome for Testing build, which Google
// publishes for every platform with no installer.
package chrome

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// The default Chrome for Testing endpoints. Tests point them elsewhere with
// the variables below.
const (
	defaultVersionsURL  = "https://googlechromelabs.github.io/chrome-for-testing"
	defaultDownloadsURL = "https://storage.googleapis.com/chrome-for-testing-public"

	versionsURLEnvVar  = "SETUP_STAGEHAND_CHROME_VERSIONS_URL"
	downloadsURLEnvVar = "SETUP_STAGEHAND_CHROME_DOWNLOADS_URL"
)

// Channels are the release channels Chrome for Testing publishes.
var Channels = []string{"stable", "beta", "dev", "canary"}

var (
	fullVersion    = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
	partialVersion = regexp.MustCompile(`^\d+(\.\d+){0,2}$`)
)

// ResolveVersion turns a channel ("stable"), a milestone ("158"), a build
// prefix ("158.0.8127") or a full version into the full version to install.
func ResolveVersion(ctx context.Context, spec string) (string, error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	if spec == "" {
		spec = "stable"
	}
	if fullVersion.MatchString(spec) {
		return spec, nil
	}
	var name string
	switch {
	case isChannel(spec):
		name = "LATEST_RELEASE_" + strings.ToUpper(spec)
	case partialVersion.MatchString(spec):
		name = "LATEST_RELEASE_" + spec
	default:
		return "", fmt.Errorf("chrome-version %q: want a channel (%s), a milestone such as 158, or a full version such as 158.0.8127.88",
			spec, strings.Join(Channels, ", "))
	}
	body, status, err := get(ctx, endpoint(versionsURLEnvVar, defaultVersionsURL)+"/"+name)
	if err != nil {
		return "", fmt.Errorf("looking up Chrome for Testing %s: %w", spec, err)
	}
	if status == http.StatusNotFound {
		return "", fmt.Errorf("chrome-version %q: Chrome for Testing has no such release", spec)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("looking up Chrome for Testing %s: HTTP %d", spec, status)
	}
	v := strings.TrimSpace(string(body))
	if !fullVersion.MatchString(v) {
		return "", fmt.Errorf("looking up Chrome for Testing %s: unexpected answer %q", spec, v)
	}
	return v, nil
}

func isChannel(s string) bool {
	for _, c := range Channels {
		if s == c {
			return true
		}
	}
	return false
}

// Platform is Chrome for Testing's name for the platform this runs on.
func Platform() (string, error) {
	return platformFor(runtime.GOOS, runtime.GOARCH)
}

func platformFor(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "linux64", nil
	case "linux/arm64":
		return "linux-arm64", nil
	case "darwin/arm64":
		return "mac-arm64", nil
	case "darwin/amd64":
		return "mac-x64", nil
	case "windows/amd64", "windows/arm64":
		// Windows on Arm runs x64 builds; Chrome for Testing has no Arm one.
		return "win64", nil
	case "windows/386":
		return "win32", nil
	}
	return "", fmt.Errorf("chrome for Testing has no build for %s/%s", goos, goarch)
}

// Build is one Chrome for Testing build for one platform.
type Build struct {
	Version  string
	Platform string
}

// URL is where the build's zip is published.
func (b Build) URL() string {
	return fmt.Sprintf("%s/%s/%s/chrome-%s.zip", endpoint(downloadsURLEnvVar, defaultDownloadsURL), b.Version, b.Platform, b.Platform)
}

// Executable is the path of the browser inside dir, where the zip was
// extracted.
func (b Build) Executable(dir string) string {
	top := filepath.Join(dir, "chrome-"+b.Platform)
	switch {
	case strings.HasPrefix(b.Platform, "mac"):
		return filepath.Join(top, "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing")
	case strings.HasPrefix(b.Platform, "win"):
		return filepath.Join(top, "chrome.exe")
	}
	return filepath.Join(top, "chrome")
}

// DebDeps is the list of Debian packages the Linux build ships with.
func (b Build) DebDeps(dir string) string {
	return filepath.Join(dir, "chrome-"+b.Platform, "deb.deps")
}

// Install downloads the build and extracts it into dir, which must not exist
// yet. It is extracted next to dir and moved into place, so a download cut
// short never leaves a dir that looks installed.
func (b Build) Install(ctx context.Context, dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return err
	}
	zipFile, err := os.CreateTemp(filepath.Dir(dir), "chrome-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(zipFile.Name())
	defer zipFile.Close()
	if err := download(ctx, b.URL(), zipFile); err != nil {
		return fmt.Errorf("downloading Chrome for Testing %s: %w", b.Version, err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(dir), ".extract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := unzip(zipFile.Name(), staging); err != nil {
		return fmt.Errorf("extracting Chrome for Testing %s: %w", b.Version, err)
	}
	if _, err := os.Stat(b.Executable(staging)); err != nil {
		return fmt.Errorf("chrome for Testing %s: the zip has no %s", b.Version, filepath.Base(b.Executable(staging)))
	}
	return os.Rename(staging, dir)
}

// Installed reports whether dir holds a complete install of the build.
func (b Build) Installed(dir string) bool {
	return IsExecutable(b.Executable(dir))
}

func endpoint(envVar, def string) string {
	if v := os.Getenv(envVar); v != "" {
		return strings.TrimRight(v, "/")
	}
	return def
}

var httpClient = &http.Client{Timeout: 30 * time.Minute}

func get(ctx context.Context, url string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, err
}

func download(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// IsExecutable reports whether path is a regular file that can be run, as
// Stagehand checks the Chrome it is given.
func IsExecutable(path string) bool {
	info, err := os.Stat(path) //nolint:gosec // only stats it
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

// ErrNotFound is returned by Find when the machine has no Chrome.
var ErrNotFound = errors.New("no Chrome found")
