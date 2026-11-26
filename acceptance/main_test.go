//go:build acceptance

// Package acceptance_test runs setup-stagehand against a real Chrome for
// Testing, downloaded from Google, and the real Stagehand from npm, driving a
// page served by the test. Tagged out of `task test` because it needs
// Node.js and the network. No model key is needed: the scripts use only
// Stagehand's page API.
package acceptance_test

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"

	"github.com/circleci-functions/setup-stagehand/helper"
)

// binaryPathEnvVar names a prebuilt setup-stagehand to test, for an executor
// with no Go toolchain to compile one with.
const binaryPathEnvVar = "SETUP_STAGEHAND_BINARY"

// The test project is embedded, so a cross-compiled suite carries it to an
// executor with no checkout.
//
//go:embed testdata/project
var testdata embed.FS

var binaryPath string

// sharedHome is the HOME the tests share, so that they download Chrome once
// between them.
var sharedHome string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	dir, err := os.MkdirTemp("", "setup-stagehand-acceptance-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	sharedHome = dir

	if bin := os.Getenv(binaryPathEnvVar); bin != "" {
		abs, err := filepath.Abs(bin)
		if err != nil {
			return 0, fmt.Errorf("%s=%q: %w", binaryPathEnvVar, bin, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return 0, fmt.Errorf("%s=%q: %w", binaryPathEnvVar, bin, err)
		}
		binaryPath = abs
		return m.Run(), nil
	}

	p := compiler.NewParallel(1)
	defer p.Cleanup()
	p.Add(compiler.Work{Result: &binaryPath, Name: "setup-stagehand", Target: "..", Source: "."})
	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// env is a test's sandbox, the test project with its dependencies installed,
// the site it tests, and the variables a job would set.
type env struct {
	sandbox *sdktest.Sandbox
	project string
	site    string
	vars    map[string]string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><title>setup-stagehand acceptance</title><h1>Shop</h1><button>Checkout</button>"))
	}))
	t.Cleanup(site.Close)
	e := &env{sandbox: sdktest.NewSandbox(t), project: project(t), site: site.URL}
	e.vars = map[string]string{
		"HOME":             sharedHome,
		"npm_config_cache": npmCache(t),
		"CI":               "true",
		"PATH":             os.Getenv("PATH"),
	}
	if runtime.GOOS == "windows" {
		for _, name := range windowsProfileEnvVars {
			e.vars[name] = os.Getenv(name)
		}
	}
	return e
}

// windowsProfileEnvVars are the variables Windows gives every process, which
// a sandbox does not pass on and Node.js fails to start without.
var windowsProfileEnvVars = []string{
	"APPDATA", "LOCALAPPDATA", "USERPROFILE", "SystemRoot", "SystemDrive",
	"windir", "TEMP", "TMP", "ComSpec", "PATHEXT", "ProgramData", "ProgramFiles", "ProgramFiles(x86)",
}

func (e *env) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	vars := e.sandbox.Env()
	maps.Copy(vars, e.vars)
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(vars))
	_, err := r.Run(append(args, "--working-directory="+e.project, "--base-url="+e.site)...)
	return r.StdoutString() + r.StderrString(), err
}

// project copies the test project and the helper, and installs Stagehand.
func project(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	assert.NilError(t, err)
	sub, err := fs.Sub(testdata, "testdata/project")
	assert.NilError(t, err)
	assert.NilError(t, os.CopyFS(dir, sub))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "stagehand-ci.ts"), helper.Source, 0o600))
	cmd := exec.Command(npm(), "install", "--no-audit", "--no-fund", "--loglevel=error")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	assert.NilError(t, err, string(out))
	return dir
}

func npmCache(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(npm(), "config", "get", "cache").Output()
	assert.NilError(t, err)
	for len(out) > 0 && (out[len(out)-1] == '\n' || out[len(out)-1] == '\r') {
		out = out[:len(out)-1]
	}
	return string(out)
}

func npm() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}
