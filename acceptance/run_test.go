//go:build acceptance

package acceptance_test

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci-functions/setup-stagehand/chrome"
)

func TestRun_DrivesChromeForTestingWithTheHelper(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--command=node home.ts", "--chrome-version=stable")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "Chrome for Testing"))
	assert.Check(t, cmp.Contains(out, "PASS: loaded setup-stagehand acceptance"))
}

func TestRun_SavesAScreenshotOfAFailure(t *testing.T) {
	e := newEnv(t)

	out, err := e.run(t, "--command=node fails.ts", "--chrome-version=stable")
	assert.Check(t, err != nil, out)
	assert.Check(t, cmp.Contains(out, "the checkout button is missing"))
	assert.Check(t, cmp.Contains(out, "checkout-page.png"))
	assert.Check(t, cmp.Contains(out, "node fails.ts failed (exit 1)"))
}

func TestRun_UsesTheJobsChrome(t *testing.T) {
	if _, err := chrome.Find(); err != nil {
		t.Skip("this executor has no Chrome of its own")
	}
	e := newEnv(t)

	out, err := e.run(t, "--command=node home.ts")
	assert.NilError(t, err, out)
	assert.Check(t, cmp.Contains(out, "using the job's Chrome"))
	assert.Check(t, cmp.Contains(out, "PASS: loaded setup-stagehand acceptance"))
}
