package fixturetest_test

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestRunErrorsOnBadWantManifest covers finding 1a: a malformed .want
// manifest is an infrastructure problem, not a fixture failure — Run must
// return a non-nil error (never a silent pass) and the error must name the
// bad token so a human can find the mistake.
func TestRunErrorsOnBadWantManifest(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                    "version: 1\n",
		".formwork/rules/r.yaml":                     testConfig,
		".formwork/fixtures/fruit-free/fire-1/f.txt": "clean\n",
		".formwork/fixtures/fruit-free/fire-1.want":  "docs/y.md:notanumber\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err == nil {
		t.Fatal("expected error for bad .want manifest, got nil (silent pass)")
	} else if !strings.Contains(err.Error(), "notanumber") {
		t.Fatalf("error %q does not mention the bad token", err.Error())
	}
}

// TestRunErrorsOnUnreadableFixtureDir covers finding 1b: if a rule's
// fixtures directory can't be read, Run must surface an error rather than
// silently skipping the rule (which would read as SKIP, not a problem).
func TestRunErrorsOnUnreadableFixtureDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission test not applicable on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits don't restrict root")
	}

	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                    "version: 1\n",
		".formwork/rules/r.yaml":                     testConfig,
		".formwork/fixtures/fruit-free/fire-1/f.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "clean\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	ruleDir := root + "/.formwork/fixtures/fruit-free"
	if err := os.Chmod(ruleDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(ruleDir, 0o755); err != nil {
			t.Fatal(err)
		}
	})

	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err == nil {
		t.Fatal("expected error for unreadable fixture rule dir, got nil (silent pass)")
	}
}

// TestRunErrorsWhenRuleCannotBeRefreshed covers finding 1c: Fresh() fails
// for a rule hand-built with config.New (no factory). config.Load-compiled
// rules always carry a factory, but config.Config.Rules is a plain exported
// []*config.Rule, so a caller of the public fixturetest.Run/config.New seam
// can still construct one directly — this is reachable without touching
// fixturetest internals.
func TestRunErrorsWhenRuleCannotBeRefreshed(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/fixtures/hand-built/fire-1/f.txt": "anything\n",
	})

	r, err := config.New("hand-built", "fake", finding.SeverityError, "",
		[]string{"**"}, nil, nil, nopChecker{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Version: 1, Rules: []*config.Rule{r}}

	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err == nil {
		t.Fatal("expected error for un-refreshable rule, got nil (silent pass)")
	} else if !strings.Contains(err.Error(), "hand-built") {
		t.Fatalf("error %q does not name the rule", err.Error())
	} else if !strings.Contains(err.Error(), "fire-1") {
		t.Fatalf("error %q does not name the fixture", err.Error())
	}
}

// TestRunErrorsOnUnrecognizedFixtureDir covers fix 1: a subdirectory of a
// rule's fixtures dir that matches neither fire-* nor pass-* (e.g. a typo'd
// fire2/) is broken fixture setup, not something to silently skip — Run must
// return a non-nil error naming the offending path.
func TestRunErrorsOnUnrecognizedFixtureDir(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                    "version: 1\n",
		".formwork/rules/r.yaml":                     testConfig,
		".formwork/fixtures/fruit-free/fire-1/f.txt": "banana want: fruit-free\n",
		".formwork/fixtures/fruit-free/pass-1/f.txt": "clean\n",
		".formwork/fixtures/fruit-free/fire2/f.txt":  "banana want: fruit-free\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var sb strings.Builder
	if _, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err == nil {
		t.Fatal("expected error for unrecognized fixture dir, got nil (silent pass)")
	} else if !strings.Contains(err.Error(), "fire2") {
		t.Fatalf("error %q does not name the unrecognized dir", err.Error())
	}
}

type nopChecker struct{}

func (nopChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
