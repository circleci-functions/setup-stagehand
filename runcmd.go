package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/circleci-functions/toolkit/sdk"
)

// The variables the step sets for the command, which the helper reads.
const (
	envBrowser      = "STAGEHAND_BROWSER"
	envModel        = "STAGEHAND_MODEL"
	envArtifactsDir = "STAGEHAND_ARTIFACTS_DIR"
	envBaseURL      = "BASE_URL"
	envAPIKey       = "BROWSERBASE_API_KEY" //nolint:gosec // the variable's name, not a credential
)

type runCmd struct {
	installFlags `embed:""`

	Command          string `required:"" help:"Shell command that runs the project's Stagehand scripts or tests, in working-directory." example:"node e2e/checkout.ts"`
	BaseURL          string `name:"base-url" help:"URL of the site to test, exported to the command as BASE_URL. Empty uses DEPLOYMENT_URL, then VERCEL_DEPLOYMENT_URL, which a deploy step exports, and with neither leaves BASE_URL alone." example:"https://my-app.example.com"`
	Browser          string `enum:"auto,local,browserbase" default:"auto" help:"Where Chrome runs, exported as STAGEHAND_BROWSER: local, in the job, or browserbase, on Browserbase, which needs BROWSERBASE_API_KEY. auto is browserbase when BROWSERBASE_API_KEY is set, else local."`
	Model            string `help:"Model for act, extract, observe and agent, exported to the command as STAGEHAND_MODEL as written, such as anthropic/claude-sonnet-5-5. The script, or the helper, decides what to do with it; the step does not check it. Empty leaves the choice to the script." example:"anthropic/claude-sonnet-5-5"`
	TestResults      string `name:"test-results" help:"JUnit XML file or directory the command writes, relative to working-directory, stored in the job's Tests tab." example:"test-results/junit.xml"`
	WorkingDirectory string `name:"working-directory" default:"." help:"Directory to run the command in, holding the project's package.json." example:"apps/web"`
}

func (c *runCmd) Run(ctx context.Context, client *sdk.Client) error {
	browser, dir, err := c.preflight()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "setup-stagehand-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	artifacts := filepath.Join(tmp, "artifacts")
	if err := os.Mkdir(artifacts, 0o750); err != nil {
		return err
	}

	env := c.env(browser, artifacts)
	if browser == "local" {
		path, err := installChrome(ctx, client, c.installFlags)
		if err != nil {
			return err
		}
		env = append(env, "CHROME_PATH="+path)
	} else {
		fmt.Fprintln(os.Stderr, "setup-stagehand: running on Browserbase, so not installing Chrome")
	}

	fmt.Fprintf(os.Stderr, "setup-stagehand: running %s\n", c.Command)
	err = runShell(ctx, dir, env, c.Command)

	// The step may have been cancelled, and what was saved is still worth
	// keeping.
	storeCtx := context.WithoutCancel(ctx)
	store(storeCtx, client, artifacts, "stagehand")
	if c.TestResults != "" {
		storeResults(storeCtx, client, inDir(dir, c.TestResults))
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s failed (exit %d)", c.Command, exitErr.ExitCode())
	}
	return err
}

// preflight checks, before anything is installed, what would make the
// command fail later: a missing Browserbase key, or a Node.js too old for the
// project's Stagehand. The model is the script's to choose and configure, so
// it is not checked. It returns where Chrome runs and the directory to run
// the command in.
func (c *runCmd) preflight() (browser, dir string, err error) {
	browser, err = c.browser()
	if err != nil {
		return "", "", err
	}
	if browser == "local" && !anyAPIKey(os.Environ()) {
		fmt.Fprintln(os.Stderr, "setup-stagehand: note: no *_API_KEY variable is set, so unless the script gets a model another way, act, extract, observe and agent will fail")
	}
	dir, err = filepath.Abs(c.WorkingDirectory)
	if err != nil {
		return "", "", err
	}
	return browser, dir, checkStagehand(dir)
}

// env is the command's environment: the job's, and what the step sets.
func (c *runCmd) env(browser, artifacts string) []string {
	env := append(os.Environ(), envBrowser+"="+browser, envArtifactsDir+"="+artifacts)
	if c.Model != "" {
		env = append(env, envModel+"="+c.Model)
	}
	if base := baseURL(c.BaseURL); base != "" {
		env = append(env, envBaseURL+"="+base)
		fmt.Fprintf(os.Stderr, "setup-stagehand: testing %s\n", base)
	}
	return env
}

// anyAPIKey reports whether any *_API_KEY variable other than Browserbase's
// is set: a hint that the script has a model to call, whichever provider or
// gateway it uses.
func anyAPIKey(environ []string) bool {
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		if value != "" && strings.HasSuffix(name, "_API_KEY") && name != envAPIKey {
			return true
		}
	}
	return false
}

// browser resolves --browser=auto, and checks Browserbase has its key.
func (c *runCmd) browser() (string, error) {
	hasKey := os.Getenv(envAPIKey) != ""
	switch c.Browser {
	case "browserbase":
		if !hasKey {
			return "", fmt.Errorf("--browser=browserbase needs %s: add it to the job, for example from a context", envAPIKey)
		}
		return "browserbase", nil
	case "local":
		return "local", nil
	}
	if hasKey {
		return "browserbase", nil
	}
	return "local", nil
}

// checkStagehand fails early on a Node.js too old for the project's
// Stagehand. A project with no Stagehand in node_modules, such as a Python
// one, is left alone.
func checkStagehand(dir string) error {
	version, ok := stagehandVersion(dir)
	if !ok {
		return nil
	}
	fmt.Fprintf(os.Stderr, "setup-stagehand: the project uses @browserbasehq/stagehand %s\n", version)
	if major(version) < 4 {
		return nil
	}
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return fmt.Errorf("stagehand %s needs Node.js 22.18 or later, and node is not on PATH: install it first, for example with setup-node", version)
	}
	node := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	if !nodeRunsStagehand4(node) {
		return fmt.Errorf("stagehand %s needs Node.js 22.18 or later, and the job has %s: use a newer image, or install one with setup-node", version, node)
	}
	return nil
}

// stagehandVersion finds @browserbasehq/stagehand as Node.js would from dir:
// in node_modules there or in a parent.
func stagehandVersion(dir string) (string, bool) {
	for d := dir; ; d = filepath.Dir(d) {
		b, err := os.ReadFile(filepath.Join(d, "node_modules", "@browserbasehq", "stagehand", "package.json")) //nolint:gosec // the project's own file
		if err == nil {
			var pkg struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(b, &pkg) == nil && pkg.Version != "" {
				return pkg.Version, true
			}
		}
		if filepath.Dir(d) == d {
			return "", false
		}
	}
}

func major(version string) int {
	n, _ := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	return n
}

// nodeRunsStagehand4 reports whether a Node.js version is 22.18 or later,
// which Stagehand 4 requires.
func nodeRunsStagehand4(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	maj, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return maj > 22 || maj == 22 && minor >= 18
}

// runShell runs command with bash where there is one, as a CircleCI run step
// does, else sh, or cmd on Windows without bash.
func runShell(ctx context.Context, dir string, env []string, command string) error {
	shell, args := shellFor(command)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Cancelling the step stops the command gently first, so the script can
	// close its browser and release a Browserbase session.
	cmd.Cancel = func() error { return interrupt(cmd.Process) }
	cmd.WaitDelay = 20 * time.Second
	return cmd.Run()
}

func shellFor(command string) (string, []string) {
	if bash, err := exec.LookPath("bash"); err == nil {
		return bash, []string{"-eo", "pipefail", "-c", command}
	}
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-ec", command}
}

func interrupt(p *os.Process) error {
	if runtime.GOOS == "windows" {
		return p.Kill()
	}
	return p.Signal(syscall.SIGINT)
}

// store keeps a directory with anything in it as artifacts. Outside a job, or
// if the store fails, it says so and carries on: a failed store should not
// hide the command's own result.
func store(ctx context.Context, client *sdk.Client, path, destination string) {
	if entries, err := os.ReadDir(path); err != nil || len(entries) == 0 {
		return
	}
	err := client.StoreArtifacts(ctx, path, destination)
	if err != nil && !errors.Is(err, sdk.ErrNoServer) {
		fmt.Fprintf(os.Stderr, "setup-stagehand: could not store %s: %v\n", destination, err)
	}
}

func storeResults(ctx context.Context, client *sdk.Client, path string) {
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "setup-stagehand: no test results at %s\n", path)
		return
	}
	err := client.StoreTestResults(ctx, path)
	if err != nil && !errors.Is(err, sdk.ErrNoServer) {
		fmt.Fprintf(os.Stderr, "setup-stagehand: could not store test results: %v\n", err)
	}
}

func inDir(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// deploymentURLEnvVars are where deploy steps leave the URL they deployed to,
// in the order they are tried: the name deploy functions share, then the one
// setup-vercel exports.
var deploymentURLEnvVars = []string{"DEPLOYMENT_URL", "VERCEL_DEPLOYMENT_URL"}

// baseURL returns the site to test, with a scheme, or "" to leave BASE_URL
// as the job has it. A bare host gets https, as deploy steps often export
// one, except a local server, which gets http.
func baseURL(flag string) string {
	base := strings.TrimSpace(flag)
	for _, name := range deploymentURLEnvVars {
		if base != "" {
			break
		}
		base = strings.TrimSpace(os.Getenv(name))
	}
	if base == "" {
		return ""
	}
	if !strings.Contains(base, "://") {
		scheme := "https://"
		if host := strings.SplitN(base, ":", 2)[0]; host == "localhost" || host == "127.0.0.1" {
			scheme = "http://"
		}
		base = scheme + base
	}
	return strings.TrimRight(base, "/")
}
