package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

const freshRules = `rules:
  - id: needs-anchor
    type: required-pattern
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'anchor'
      mode: exists
`

func TestFreshReturnsIndependentCheckers(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml":  freshRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Rules[0]

	fresh1, err := r.Fresh()
	if err != nil {
		t.Fatal(err)
	}
	fresh2, err := r.Fresh()
	if err != nil {
		t.Fatal(err)
	}
	if fresh1.Checker == fresh2.Checker || fresh1.Checker == r.Checker {
		t.Fatal("Fresh did not construct a new checker instance")
	}
	if fresh1.ID != r.ID || !fresh1.Applies("a.txt") {
		t.Fatalf("Fresh lost envelope state: %+v", fresh1)
	}

	// Behavioral independence: satisfy exists-mode on fresh1 only; fresh2
	// must not have observed anything and so must pass Finalize quietly.
	if _, err := fresh1.Checker.CheckFile(scan.NewMemFile("a.txt", []byte("anchor\n"))); err != nil {
		t.Fatal(err)
	}
	if ms := fresh2.Checker.(rules.Finalizer).Finalize(); len(ms) != 0 {
		t.Fatalf("state leaked between fresh checkers: %+v", ms)
	}
}

func TestFreshErrorsForConstructorBuiltRules(t *testing.T) {
	r, err := config.New("hand-built", "fake", finding.SeverityError, "", []string{"**"}, nil, nil, nopChecker{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Fresh(); err == nil || !strings.Contains(err.Error(), "hand-built") {
		t.Fatalf("expected error naming the rule, got %v", err)
	}
}

type nopChecker struct{}

func (nopChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
