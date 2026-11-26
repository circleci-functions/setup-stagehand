package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/circleci-functions/toolkit/bashenv"
	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-stagehand/chrome"
)

// installFlags say which Chrome to put on the job, for both commands.
type installFlags struct {
	ChromeVersion string `name:"chrome-version" help:"Chrome for Testing to install: a channel (stable, beta, dev, canary), a milestone such as \"158\", or a full version. Empty uses a Chrome the job already has, where Stagehand would find it, and installs stable if there is none." example:"stable"`
	WithDeps      bool   `name:"with-deps" default:"true" help:"On Debian and Ubuntu, also install the system packages a downloaded Chrome needs, with sudo. Already installed ones are skipped."`
	Cache         bool   `default:"true" help:"Restore the downloaded Chrome from the job cache, and save it after a fresh download."`
}

type installCmd struct {
	installFlags `embed:""`
}

func (c *installCmd) Run(ctx context.Context, client *sdk.Client) error {
	path, err := installChrome(ctx, client, c.installFlags)
	if err != nil {
		return err
	}
	err = bashenv.ExportEnvVars(map[string]string{"CHROME_PATH": path})
	if errors.Is(err, bashenv.ErrNoBashEnv) {
		fmt.Fprintf(os.Stderr, "setup-stagehand: not in a job, so not exporting CHROME_PATH=%s\n", path)
		return nil
	}
	return err
}

// installChrome finds or installs the Chrome to hand Stagehand, and returns
// the path to set CHROME_PATH to.
func installChrome(ctx context.Context, client *sdk.Client, f installFlags) (string, error) {
	path, err := chromeFor(ctx, client, f)
	if err != nil {
		return "", err
	}
	if !chrome.NeedsHeadless() {
		return path, nil
	}
	root, err := stateDir()
	if err != nil {
		return "", err
	}
	wrapper := filepath.Join(root, "headless", "chrome")
	if err := chrome.WriteHeadlessWrapper(wrapper, path); err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr, "setup-stagehand: there is no display, so Chrome runs headless")
	return wrapper, nil
}

func chromeFor(ctx context.Context, client *sdk.Client, f installFlags) (string, error) {
	if strings.TrimSpace(f.ChromeVersion) == "" {
		if path, err := chrome.Find(); err == nil {
			fmt.Fprintf(os.Stderr, "setup-stagehand: using the job's Chrome at %s\n", path)
			return path, nil
		}
	}
	platform, err := chrome.Platform()
	if err != nil {
		return "", err
	}
	version, err := chrome.ResolveVersion(ctx, f.ChromeVersion)
	if err != nil {
		return "", err
	}
	b := chrome.Build{Version: version, Platform: platform}
	root, err := stateDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "chrome", version, platform)
	key := fmt.Sprintf("setup-stagehand-chrome-v1-%s-%s", sdk.OSArch(), version)

	source := "already installed"
	if !b.Installed(dir) && f.Cache {
		_, err := client.RestoreCache(ctx, []string{key}, sdk.WithStepName("Restoring Chrome for Testing"))
		sdk.LogCacheErr("restore", err)
		source = "restored from the cache"
	}
	if !b.Installed(dir) {
		source = "downloaded"
		_ = os.RemoveAll(dir)
		if err := retry(ctx, "download Chrome for Testing", func() error {
			_ = os.RemoveAll(dir)
			return b.Install(ctx, dir)
		}); err != nil {
			return "", err
		}
		if f.Cache {
			sdk.LogCacheErr("save", client.SaveCache(ctx, key, []string{dir}, sdk.WithStepName("Saving Chrome for Testing")))
		}
	}
	fmt.Fprintf(os.Stderr, "setup-stagehand: Chrome for Testing %s %s: %s\n", version, source, b.Executable(dir))
	if runtime.GOOS == "linux" && f.WithDeps {
		if err := installDeps(ctx, b.DebDeps(dir)); err != nil {
			return "", err
		}
	}
	return b.Executable(dir), nil
}

// installDeps installs the system packages Chrome's deb.deps lists that the
// machine does not have yet.
func installDeps(ctx context.Context, debDeps string) error {
	apt := chrome.HostApt()
	if !apt.Available() {
		fmt.Fprintln(os.Stderr, "setup-stagehand: no apt-get here, so not installing Chrome's system packages; with-deps covers Debian and Ubuntu")
		return nil
	}
	file, err := os.Open(debDeps) //nolint:gosec // inside the build just installed
	if err != nil {
		return err
	}
	defer file.Close()
	reqs, err := chrome.ParseDebDeps(file)
	if err != nil {
		return err
	}
	missing := apt.Missing(ctx, reqs)
	if len(missing) == 0 {
		fmt.Fprintln(os.Stderr, "setup-stagehand: Chrome's system packages are already installed")
		return nil
	}
	return retry(ctx, "install Chrome's system packages", func() error { return apt.Install(ctx, missing) })
}

// stateDir is where setup-stagehand keeps Chrome builds and the headless
// wrapper: the user's cache directory, which outlives the step.
func stateDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "setup-stagehand"), nil
}

// retryDelays are the waits between attempts at a download or package
// install: CDNs and package mirrors fail now and then.
var retryDelays = []time.Duration{15 * time.Second, 30 * time.Second}

func retry(ctx context.Context, what string, fn func() error) error {
	delays := retryDelays
	if d, err := time.ParseDuration(os.Getenv("SETUP_STAGEHAND_RETRY_DELAY")); err == nil {
		delays = []time.Duration{d, d}
	}
	err := fn()
	for _, d := range delays {
		if err == nil || ctx.Err() != nil {
			break
		}
		fmt.Fprintf(os.Stderr, "setup-stagehand: could not %s, trying again in %s: %v\n", what, d, err)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
		err = fn()
	}
	return err
}
