package clitest_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

var stable = []string{"install", "--chrome-version=stable", "--with-deps=false"}

func TestInstall_DownloadsCachesAndExportsChrome(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, stable...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "Chrome for Testing "+version+" downloaded"))
	path := e.exported(t)["CHROME_PATH"]
	assert.Check(t, strings.Contains(path, filepath.Join("setup-stagehand", "chrome", version)), path)
	got, err := exec(path)
	assert.NilError(t, err)
	assert.Check(t, cmp.Contains(got, version))
	keys := e.sandbox.Cache.SavedKeys()
	assert.Assert(t, cmp.Len(keys, 1))
	assert.Check(t, cmp.Contains(keys[0], "setup-stagehand-chrome-v1-"))
	assert.Check(t, cmp.Contains(keys[0], version))

	out, err = e.run(t, stable...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "already installed"))
	assert.Check(t, cmp.Equal(e.downloads.Load(), int32(1)))
}

func TestInstall_RestoresChromeFromTheCache(t *testing.T) {
	e := newEnv(t)
	out, err := e.run(t, stable...)
	assert.NilError(t, err, out)
	assert.NilError(t, os.RemoveAll(filepath.Join(e.sandbox.Home, ".cache", "setup-stagehand")))
	assert.NilError(t, os.RemoveAll(filepath.Join(e.sandbox.Home, "Library", "Caches", "setup-stagehand")))

	out, err = e.run(t, stable...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "restored from the cache"))
	assert.Check(t, cmp.Equal(e.downloads.Load(), int32(1)))
}

func TestInstall_UsesTheJobsChrome(t *testing.T) {
	e := newEnv(t)
	mine := filepath.Join(t.TempDir(), "chrome")
	assert.NilError(t, os.WriteFile(mine, []byte(fakeChrome), 0o755)) //nolint:gosec // an executable script
	e.vars["CHROME_PATH"] = mine

	out, err := e.run(t, "install")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "using the job's Chrome at "+mine))
	assert.Check(t, cmp.Equal(e.exported(t)["CHROME_PATH"], mine))
	assert.Check(t, cmp.Equal(e.downloads.Load(), int32(0)))
}

func TestInstall_RunsChromeHeadlessWithNoDisplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux runs without a display")
	}
	e := newEnv(t)
	delete(e.vars, "DISPLAY")

	out, err := e.run(t, stable...)
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "there is no display, so Chrome runs headless"))
	wrapper := e.exported(t)["CHROME_PATH"]
	assert.Check(t, strings.HasSuffix(wrapper, filepath.Join("setup-stagehand", "headless", "chrome")), wrapper)
	script, err := os.ReadFile(wrapper) //nolint:gosec // the test's own file
	assert.NilError(t, err)
	assert.Check(t, cmp.Contains(string(script), "--headless=new"))
}

func TestInstall_RejectsAnUnknownVersion(t *testing.T) {
	e := newEnv(t)
	out, err := e.run(t, "install", "--chrome-version=nightly")
	assert.Check(t, err != nil)
	assert.Check(t, cmp.Contains(out, `chrome-version "nightly": want a channel`))
}
