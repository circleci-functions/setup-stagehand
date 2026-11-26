package chrome

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

const debDeps = `ca-certificates
libasound2 (>= 1.0.17)
libcurl3-gnutls | libcurl3-nss | libcurl4 | libcurl3
libnss3 (>= 2:3.35)
wget
xdg-utils (>= 1.0.2)
`

func TestParseDebDeps(t *testing.T) {
	reqs, err := ParseDebDeps(strings.NewReader(debDeps))
	assert.NilError(t, err)
	assert.Check(t, cmp.DeepEqual(reqs, []Requirement{
		{"ca-certificates"},
		{"libasound2"},
		{"libcurl3-gnutls", "libcurl3-nss", "libcurl4", "libcurl3"},
		{"libnss3"},
	}))
}

// fakeApt answers as a machine with installed packages, and apt-cache
// candidates for the rest, and records the commands it ran.
type fakeApt struct {
	installed  map[string]bool
	candidates map[string]bool
	ran        []string
}

func (f *fakeApt) apt() Apt {
	return Apt{Log: io.Discard, Run: func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		f.ran = append(f.ran, line)
		pkg := args[len(args)-1]
		switch name {
		case "dpkg-query":
			if f.installed[pkg] {
				return "ii ", nil
			}
			return "", errors.New("not installed")
		case "apt-cache":
			if f.candidates[pkg] {
				return pkg + ":\n  Installed: (none)\n  Candidate: 1.0\n", nil
			}
			return pkg + ":\n  Installed: (none)\n  Candidate: (none)\n", nil
		}
		return "", nil
	}}
}

func TestApt_InstallsOnlyWhatIsMissing(t *testing.T) {
	reqs, err := ParseDebDeps(strings.NewReader(debDeps))
	assert.NilError(t, err)
	f := &fakeApt{
		// t64 packages replaced the old names on Ubuntu 24.04.
		installed:  map[string]bool{"ca-certificates": true, "libcurl4t64": true},
		candidates: map[string]bool{"libasound2t64": true, "libnss3": true},
	}
	a := f.apt()
	ctx := context.Background()

	missing := a.Missing(ctx, reqs)
	assert.Check(t, cmp.DeepEqual(missing, []Requirement{{"libasound2"}, {"libnss3"}}))

	f.ran = nil
	assert.NilError(t, a.Install(ctx, missing))
	var apt []string
	for _, r := range f.ran {
		if strings.Contains(r, "apt-get") {
			apt = append(apt, r)
		}
	}
	assert.Assert(t, cmp.Len(apt, 2))
	assert.Check(t, strings.HasSuffix(apt[0], "apt-get update -qq"), apt[0])
	assert.Check(t, strings.HasSuffix(apt[1], "apt-get install -y -qq --no-install-recommends libasound2t64 libnss3"), apt[1])
}

func TestApt_InstallsNothingWhenNothingIsMissing(t *testing.T) {
	f := &fakeApt{}
	assert.NilError(t, f.apt().Install(context.Background(), nil))
	assert.Check(t, cmp.Len(f.ran, 0))
}

func TestApt_ReportsAPackageNoOneProvides(t *testing.T) {
	f := &fakeApt{}
	err := f.apt().Install(context.Background(), []Requirement{{"libgtk-3-0", "libgtk-4-1"}})
	assert.Check(t, cmp.ErrorContains(err, "no package provides libgtk-3-0 or libgtk-4-1 here"))
}

func TestHasCandidate(t *testing.T) {
	assert.Check(t, hasCandidate("x:\n  Installed: (none)\n  Candidate: 2.1-1\n"))
	assert.Check(t, !hasCandidate("x:\n  Installed: (none)\n  Candidate: (none)\n"))
	assert.Check(t, !hasCandidate(""))
}
