package chrome

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// skippedDeps are packages in Chrome's deb.deps that only its own .deb
// installer needs, for updates and desktop integration.
var skippedDeps = map[string]bool{"wget": true, "xdg-utils": true}

// Requirement is one line of deb.deps: packages, any one of which will do.
type Requirement []string

// ParseDebDeps reads Chrome's deb.deps: one requirement per line, with
// alternatives separated by |, and version constraints, which are dropped.
func ParseDebDeps(r io.Reader) ([]Requirement, error) {
	var reqs []Requirement
	s := bufio.NewScanner(r)
	for s.Scan() {
		var req Requirement
		for _, alt := range strings.Split(s.Text(), "|") {
			name, _, _ := strings.Cut(alt, "(")
			name = strings.TrimSpace(name)
			if name != "" && !skippedDeps[name] {
				req = append(req, name)
			}
		}
		if len(req) > 0 {
			reqs = append(reqs, req)
		}
	}
	return reqs, s.Err()
}

// names are the packages that satisfy a requirement on name: name itself,
// then the t64 package that replaced it on Debian 13 and Ubuntu 24.04.
func names(name string) []string {
	return []string{name, name + "t64"}
}

// Apt runs the package tools. Tests swap Run out.
type Apt struct {
	// Run runs a command and returns its output.
	Run func(ctx context.Context, name string, args ...string) (string, error)
	// Log is where progress goes.
	Log io.Writer
}

// HostApt runs the real apt-get and dpkg-query, with sudo when not root.
func HostApt() Apt {
	return Apt{Run: runCommand, Log: os.Stderr}
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Available reports whether this machine uses apt.
func (a Apt) Available() bool {
	_, err := exec.LookPath("apt-get")
	return err == nil
}

// Missing returns the requirements no installed package satisfies.
func (a Apt) Missing(ctx context.Context, reqs []Requirement) []Requirement {
	var missing []Requirement
	for _, req := range reqs {
		if !a.anyInstalled(ctx, req) {
			missing = append(missing, req)
		}
	}
	return missing
}

func (a Apt) anyInstalled(ctx context.Context, req Requirement) bool {
	for _, n := range req {
		for _, c := range names(n) {
			out, err := a.Run(ctx, "dpkg-query", "-W", "-f=${db:Status-Abbrev}", c)
			if err == nil && strings.HasPrefix(out, "ii") {
				return true
			}
		}
	}
	return false
}

// Install refreshes the package lists and installs, for each requirement,
// the first package apt can install.
func (a Apt) Install(ctx context.Context, reqs []Requirement) error {
	if len(reqs) == 0 {
		return nil
	}
	if out, err := a.asRoot(ctx, "apt-get", "update", "-qq"); err != nil {
		return fmt.Errorf("apt-get update: %w\n%s", err, out)
	}
	var pkgs []string
	for _, req := range reqs {
		p, ok := a.candidate(ctx, req)
		if !ok {
			return fmt.Errorf("no package provides %s here", strings.Join(req, " or "))
		}
		pkgs = append(pkgs, p)
	}
	fmt.Fprintf(a.Log, "setup-stagehand: installing the packages Chrome needs: %s\n", strings.Join(pkgs, " "))
	args := append([]string{"install", "-y", "-qq", "--no-install-recommends"}, pkgs...)
	if out, err := a.asRoot(ctx, "apt-get", args...); err != nil {
		return fmt.Errorf("apt-get install: %w\n%s", err, out)
	}
	return nil
}

func (a Apt) candidate(ctx context.Context, req Requirement) (string, bool) {
	for _, n := range req {
		for _, c := range names(n) {
			out, err := a.Run(ctx, "apt-cache", "policy", c)
			if err == nil && hasCandidate(out) {
				return c, true
			}
		}
	}
	return "", false
}

// hasCandidate reads `apt-cache policy`: a package apt can install has a
// "Candidate:" version, and a virtual or unknown one has none.
func hasCandidate(policy string) bool {
	for _, line := range strings.Split(policy, "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "Candidate:")
		if ok {
			v = strings.TrimSpace(v)
			return v != "" && v != "(none)"
		}
	}
	return false
}

// asRoot runs a command as root: directly when this process is root, else
// through sudo, keeping the environment so apt-get stays non-interactive.
func (a Apt) asRoot(ctx context.Context, name string, args ...string) (string, error) {
	if os.Geteuid() == 0 {
		return a.Run(ctx, name, args...)
	}
	return a.Run(ctx, "sudo", append([]string{"-E", name}, args...)...)
}
