package chrome

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestPlatformFor(t *testing.T) {
	for goosArch, want := range map[string]string{
		"linux/amd64":   "linux64",
		"linux/arm64":   "linux-arm64",
		"darwin/arm64":  "mac-arm64",
		"darwin/amd64":  "mac-x64",
		"windows/amd64": "win64",
		"windows/arm64": "win64",
		"windows/386":   "win32",
	} {
		goos, goarch, _ := strings.Cut(goosArch, "/")
		got, err := platformFor(goos, goarch)
		assert.NilError(t, err)
		assert.Check(t, cmp.Equal(got, want), goosArch)
	}
	_, err := platformFor("linux", "s390x")
	assert.Check(t, cmp.ErrorContains(err, "no build for linux/s390x"))
}

// versions serves Chrome for Testing's LATEST_RELEASE_* files.
func versions(t *testing.T, files map[string]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(v + "\n"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(versionsURLEnvVar, srv.URL)
}

func TestResolveVersion(t *testing.T) {
	versions(t, map[string]string{
		"LATEST_RELEASE_STABLE":     "158.0.8127.88",
		"LATEST_RELEASE_BETA":       "159.0.8154.33",
		"LATEST_RELEASE_158":        "158.0.8127.88",
		"LATEST_RELEASE_158.0.8127": "158.0.8127.88",
		"LATEST_RELEASE_CANARY":     "not a version",
	})
	ctx := context.Background()
	for spec, want := range map[string]string{
		"":              "158.0.8127.88",
		"stable":        "158.0.8127.88",
		" Beta ":        "159.0.8154.33",
		"158":           "158.0.8127.88",
		"158.0.8127":    "158.0.8127.88",
		"157.0.7000.12": "157.0.7000.12",
	} {
		got, err := ResolveVersion(ctx, spec)
		assert.NilError(t, err, spec)
		assert.Check(t, cmp.Equal(got, want), spec)
	}

	_, err := ResolveVersion(ctx, "latest")
	assert.Check(t, cmp.ErrorContains(err, `chrome-version "latest": want a channel (stable, beta, dev, canary)`))
	_, err = ResolveVersion(ctx, "99")
	assert.Check(t, cmp.ErrorContains(err, "has no such release"))
	_, err = ResolveVersion(ctx, "canary")
	assert.Check(t, cmp.ErrorContains(err, `unexpected answer "not a version"`))
}

func TestBuild_Paths(t *testing.T) {
	assert.Check(t, cmp.Equal(Build{Version: "1.2.3.4", Platform: "linux64"}.URL(),
		defaultDownloadsURL+"/1.2.3.4/linux64/chrome-linux64.zip"))
	assert.Check(t, cmp.Equal(Build{Platform: "linux-arm64"}.Executable("d"), filepath.Join("d", "chrome-linux-arm64", "chrome")))
	assert.Check(t, cmp.Equal(Build{Platform: "win64"}.Executable("d"), filepath.Join("d", "chrome-win64", "chrome.exe")))
	assert.Check(t, cmp.Equal(Build{Platform: "mac-arm64"}.Executable("d"),
		filepath.Join("d", "chrome-mac-arm64", "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing")))
}

type entry struct {
	name, body string
	mode       os.FileMode
}

func zipOf(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.SetMode(e.mode)
		f, err := w.CreateHeader(h)
		assert.NilError(t, err)
		_, err = f.Write([]byte(e.body))
		assert.NilError(t, err)
	}
	assert.NilError(t, w.Close())
	return buf.Bytes()
}

func serveZip(t *testing.T, body []byte) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/1.2.3.4/linux64/chrome-linux64.zip") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(downloadsURLEnvVar, srv.URL)
}

func TestInstall_ExtractsTheBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and links")
	}
	serveZip(t, zipOf(t,
		entry{name: "chrome-linux64/", mode: os.ModeDir | 0o755},
		entry{name: "chrome-linux64/chrome", body: "#!/bin/sh\n", mode: 0o755},
		entry{name: "chrome-linux64/deb.deps", body: "libnss3\n", mode: 0o644},
		entry{name: "chrome-linux64/lib/libfoo.so.1", body: "elf", mode: 0o644},
		entry{name: "chrome-linux64/lib/libfoo.so", body: "libfoo.so.1", mode: os.ModeSymlink | 0o777},
	))
	b := Build{Version: "1.2.3.4", Platform: "linux64"}
	dir := filepath.Join(t.TempDir(), "chrome", "1.2.3.4")
	assert.Check(t, !b.Installed(dir))

	assert.NilError(t, b.Install(context.Background(), dir))
	assert.Check(t, b.Installed(dir))
	target, err := os.Readlink(filepath.Join(dir, "chrome-linux64", "lib", "libfoo.so"))
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(target, "libfoo.so.1"))
	// Nothing is left beside it.
	left, err := os.ReadDir(filepath.Dir(dir))
	assert.NilError(t, err)
	assert.Check(t, cmp.Len(left, 1))
}

func TestInstall_RefusesAZipWithoutChrome(t *testing.T) {
	serveZip(t, zipOf(t, entry{name: "chrome-linux64/ABOUT", body: "x", mode: 0o644}))
	dir := filepath.Join(t.TempDir(), "chrome")
	err := Build{Version: "1.2.3.4", Platform: "linux64"}.Install(context.Background(), dir)
	assert.Check(t, cmp.ErrorContains(err, "the zip has no chrome"))
	_, statErr := os.Stat(dir)
	assert.Check(t, os.IsNotExist(statErr))
}

func TestInstall_RefusesEntriesOutsideTheZip(t *testing.T) {
	for _, e := range []entry{
		{name: "../evil", body: "x", mode: 0o644},
		{name: "chrome-linux64/link", body: "../../etc/passwd", mode: os.ModeSymlink | 0o777},
	} {
		serveZip(t, zipOf(t, e))
		err := Build{Version: "1.2.3.4", Platform: "linux64"}.Install(context.Background(), filepath.Join(t.TempDir(), "c"))
		assert.Check(t, cmp.ErrorContains(err, "outside"), e.name)
	}
}

func TestInstall_ReportsAFailedDownload(t *testing.T) {
	serveZip(t, nil)
	err := Build{Version: "9.9.9.9", Platform: "linux64"}.Install(context.Background(), filepath.Join(t.TempDir(), "c"))
	assert.Check(t, cmp.ErrorContains(err, "HTTP 404"))
}

func TestCandidates(t *testing.T) {
	env := map[string]string{"PATH": "/a" + string(os.PathListSeparator) + "/b", "LOCALAPPDATA": `C:\Users\me\AppData\Local`}
	linux := candidates("linux", func(k string) string { return env[k] })
	assert.Check(t, cmp.DeepEqual(linux[:3], []string{
		filepath.Join("/a", "google-chrome-stable"), filepath.Join("/b", "google-chrome-stable"), filepath.Join("/a", "google-chrome"),
	}))
	assert.Check(t, cmp.Len(candidates("windows", func(k string) string { return env[k] }), 2))
	assert.Check(t, cmp.Len(candidates("darwin", nil), 4))
}

func TestFind_PrefersChromePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bits")
	}
	chrome := filepath.Join(t.TempDir(), "my-chrome")
	assert.NilError(t, os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o755)) //nolint:gosec // an executable script
	t.Setenv("CHROME_PATH", chrome)
	got, err := Find()
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(got, chrome))
}

func TestWriteHeadlessWrapper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	chrome := filepath.Join(dir, "it's chrome")
	assert.NilError(t, os.WriteFile(chrome, []byte("#!/bin/sh\necho \"$@\" > '"+args+"'\n"), 0o755)) //nolint:gosec // an executable script
	wrapper := filepath.Join(dir, "headless", "chrome")
	assert.NilError(t, WriteHeadlessWrapper(wrapper, chrome))
	assert.Check(t, IsExecutable(wrapper))

	out, err := runCommand(context.Background(), wrapper, "--remote-debugging-port=1", "about:blank")
	assert.NilError(t, err, out)
	got, err := os.ReadFile(args)
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(string(got), "--headless=new --remote-debugging-port=1 about:blank\n"))
}

func TestWriteHeadlessWrapper_RefusesToRunItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chrome")
	assert.Check(t, cmp.ErrorContains(WriteHeadlessWrapper(path, path), "would run itself"))
}
