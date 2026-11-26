// Package clitest_test exercises the setup-stagehand CLI contract against a
// fake Chrome for Testing server, whose "Chrome" is a shell script: what it
// downloads, caches and exports, and what the command it runs is given.
// Hermetic, so it runs under `task test`. The real Chrome and Stagehand are
// covered by acceptance/.
package clitest_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/bashenv"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

var binaryPath string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	p := compiler.NewParallel(1)
	defer p.Cleanup()
	p.Add(compiler.Work{Result: &binaryPath, Name: "setup-stagehand", Target: "..", Source: "."})
	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

const version = "158.0.8127.88"

// fakeChrome stands in for the browser: it says which version it is.
const fakeChrome = "#!/bin/sh\necho \"Google Chrome for Testing " + version + "\"\n"

type env struct {
	sandbox   *sdktest.Sandbox
	project   string
	downloads *atomic.Int32
	vars      map[string]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake Chrome is a shell script")
	}
	project, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	e := &env{sandbox: sdktest.NewSandbox(t), project: project, downloads: &atomic.Int32{}}
	srv := httptest.NewServer(e.cft(t))
	t.Cleanup(srv.Close)
	e.vars = map[string]string{
		"SETUP_STAGEHAND_CHROME_VERSIONS_URL":  srv.URL,
		"SETUP_STAGEHAND_CHROME_DOWNLOADS_URL": srv.URL,
		"SETUP_STAGEHAND_RETRY_DELAY":          "10ms",
		"XDG_CACHE_HOME":                       filepath.Join(e.sandbox.Home, ".cache"),
		// A display, so Chrome is not wrapped to run headless, except in
		// the tests that take it away.
		"DISPLAY": ":0",
		"PATH":    os.Getenv("PATH"),
	}
	return e
}

// cft serves Chrome for Testing: the stable version, and a zip holding the
// fake Chrome for this platform.
func (e *env) cft(t *testing.T) http.Handler {
	t.Helper()
	platform := map[string]string{"linux/amd64": "linux64", "linux/arm64": "linux-arm64", "darwin/arm64": "mac-arm64", "darwin/amd64": "mac-x64"}[runtime.GOOS+"/"+runtime.GOARCH]
	exe := "chrome-" + platform + "/chrome"
	if strings.HasPrefix(platform, "mac") {
		exe = "chrome-" + platform + "/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: exe, Method: zip.Deflate}
	h.SetMode(0o755)
	f, err := w.CreateHeader(h)
	assert.NilError(t, err)
	_, err = f.Write([]byte(fakeChrome))
	assert.NilError(t, err)
	h = &zip.FileHeader{Name: "chrome-" + platform + "/deb.deps"}
	h.SetMode(0o644)
	f, err = w.CreateHeader(h)
	assert.NilError(t, err)
	_, err = f.Write([]byte("libnss3\n"))
	assert.NilError(t, err)
	assert.NilError(t, w.Close())
	zipPath := fmt.Sprintf("/%s/%s/chrome-%s.zip", version, platform, platform)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/LATEST_RELEASE_STABLE":
			_, _ = w.Write([]byte(version))
		case zipPath:
			e.downloads.Add(1)
			_, _ = w.Write(buf.Bytes())
		default:
			http.NotFound(w, r)
		}
	})
}

func (e *env) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	vars := e.sandbox.Env()
	maps.Copy(vars, e.vars)
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(vars), runner.WithCmdDir(e.project))
	_, err := r.Run(args...)
	return r.StdoutString() + r.StderrString(), err
}

// exported returns what the step exported to later steps.
func (e *env) exported(t *testing.T) map[string]string {
	t.Helper()
	vars, err := bashenv.Vars(e.sandbox.BashEnv, nil)
	if os.IsNotExist(err) {
		return map[string]string{}
	}
	assert.NilError(t, err)
	return vars
}

// artifacts returns the stored artifacts, destination to path.
func (e *env) artifacts() map[string]string {
	got := map[string]string{}
	for _, a := range e.sandbox.Cache.StoredArtifacts() {
		got[a.Destination] = a.Path
	}
	return got
}

// dumpEnv is a command that writes the variables the step gives it, one per
// line, to the file named by its argument.
func dumpEnv(file string) string {
	return fmt.Sprintf(`for v in BASE_URL STAGEHAND_BROWSER STAGEHAND_MODEL STAGEHAND_ARTIFACTS_DIR CHROME_PATH; do eval "echo $v=\${$v-}"; done > %q`, file)
}

func readVars(t *testing.T, file string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(file)
	assert.NilError(t, err)
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	return got
}
