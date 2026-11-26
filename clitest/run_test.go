package clitest_test

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func exec(path string, args ...string) (string, error) {
	out, err := osexec.Command(path, args...).CombinedOutput() //nolint:gosec // the test's own binary
	return string(out), err
}

func TestRun_GivesTheCommandTheJobsSetup(t *testing.T) {
	e := newEnv(t)
	e.vars["DEPLOYMENT_URL"] = "my-app.example.com/"
	vars := filepath.Join(t.TempDir(), "vars")

	// No key for the model: the step passes it on and leaves it to the script.
	out, err := e.run(t, "--command="+dumpEnv(vars), "--model=anthropic/claude-sonnet-5-5", "--chrome-version=stable", "--with-deps=false")
	assert.NilError(t, err, out)
	got := readVars(t, vars)
	assert.Check(t, cmp.Equal(got["BASE_URL"], "https://my-app.example.com"))
	assert.Check(t, cmp.Equal(got["STAGEHAND_BROWSER"], "local"))
	assert.Check(t, cmp.Equal(got["STAGEHAND_MODEL"], "anthropic/claude-sonnet-5-5"))
	assert.Check(t, cmp.Contains(got["CHROME_PATH"], filepath.Join("setup-stagehand", "chrome", version)))
	assert.Check(t, got["STAGEHAND_ARTIFACTS_DIR"] != "")
	assert.Check(t, cmp.Contains(out, "testing https://my-app.example.com"))
}

func TestRun_StoresArtifactsAndTestResultsWhenTheCommandFails(t *testing.T) {
	e := newEnv(t)
	assert.NilError(t, os.MkdirAll(filepath.Join(e.project, "results"), 0o750))
	cmd := `echo png > "$STAGEHAND_ARTIFACTS_DIR/home.png"; echo '<testsuites/>' > results/junit.xml; exit 3`

	out, err := e.run(t, "--command="+cmd, "--test-results=results/junit.xml", "--chrome-version=stable", "--with-deps=false")
	assert.Check(t, err != nil)
	assert.Check(t, cmp.Contains(out, "failed (exit 3)"))
	assert.Check(t, cmp.Contains(e.artifacts(), "stagehand"))
	assert.Check(t, cmp.DeepEqual(e.sandbox.Cache.StoredTestResultPaths(), []string{filepath.Join(e.project, "results", "junit.xml")}))
}

func TestRun_StoresNoEmptyArtifacts(t *testing.T) {
	e := newEnv(t)
	out, err := e.run(t, "--command=true", "--chrome-version=stable", "--with-deps=false")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Len(e.artifacts(), 0))
	assert.Check(t, cmp.Contains(out, "no *_API_KEY variable is set"))
}

func TestRun_OnBrowserbaseInstallsNoChrome(t *testing.T) {
	e := newEnv(t)
	e.vars["BROWSERBASE_API_KEY"] = "bb_test"
	vars := filepath.Join(t.TempDir(), "vars")

	out, err := e.run(t, "--command="+dumpEnv(vars))
	assert.NilError(t, err, out)
	got := readVars(t, vars)
	assert.Check(t, cmp.Equal(got["STAGEHAND_BROWSER"], "browserbase"))
	assert.Check(t, cmp.Equal(got["CHROME_PATH"], ""))
	assert.Check(t, cmp.Equal(e.downloads.Load(), int32(0)))
	assert.Check(t, cmp.Contains(out, "running on Browserbase, so not installing Chrome"))
}

func TestRun_PassesAnyModelThrough(t *testing.T) {
	e := newEnv(t)
	e.vars["OPENROUTER_API_KEY"] = "sk-or-test"
	vars := filepath.Join(t.TempDir(), "vars")

	out, err := e.run(t, "--command="+dumpEnv(vars), "--model=openrouter/qwen/qwen3-coder", "--chrome-version=stable", "--with-deps=false")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Equal(readVars(t, vars)["STAGEHAND_MODEL"], "openrouter/qwen/qwen3-coder"))
	assert.Check(t, !strings.Contains(out, "no *_API_KEY variable is set"), out)
}

func TestRun_FailsFastWithoutABrowserbaseKey(t *testing.T) {
	e := newEnv(t)
	out, err := e.run(t, "--command=true", "--browser=browserbase")
	assert.Check(t, err != nil)
	assert.Check(t, cmp.Contains(out, "needs BROWSERBASE_API_KEY"))
	assert.Check(t, cmp.Equal(e.downloads.Load(), int32(0)))
}

func TestRun_FailsFastOnANodeTooOldForStagehand(t *testing.T) {
	e := newEnv(t)
	pkg := filepath.Join(e.project, "node_modules", "@browserbasehq", "stagehand")
	assert.NilError(t, os.MkdirAll(pkg, 0o750))
	assert.NilError(t, os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version": "4.4.0"}`), 0o600))
	bin := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\necho v20.11.1\n"), 0o755)) //nolint:gosec // an executable script
	e.vars["PATH"] = bin + string(os.PathListSeparator) + e.vars["PATH"]

	out, err := e.run(t, "--command=true")
	assert.Check(t, err != nil)
	assert.Check(t, cmp.Contains(out, "stagehand 4.4.0 needs Node.js 22.18 or later, and the job has 20.11.1"))
}
