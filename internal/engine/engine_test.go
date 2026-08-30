package engine_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type fakeChecker struct {
	match  func(f *scan.File) []rules.Match
	err    error
	panics bool
	final  []rules.Match
}

func (c *fakeChecker) CheckFile(f *scan.File) ([]rules.Match, error) {
	if c.panics {
		panic("boom")
	}
	if c.err != nil {
		return nil, c.err
	}
	if c.match == nil {
		return nil, nil
	}
	return c.match(f), nil
}

type fakeFinalizer struct{ fakeChecker }

func (c *fakeFinalizer) Finalize() []rules.Match { return c.final }

func mustRule(t *testing.T, id string, sev finding.Severity, include []string, c rules.Checker) *config.Rule {
	t.Helper()
	r, err := config.New(id, "fake", sev, "fix it", include, nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func memFileSet(files map[string]string) *scan.FileSet {
	fset := &scan.FileSet{Root: "mem"}
	for path, content := range files {
		fset.Files = append(fset.Files, scan.NewMemFile(path, []byte(content)))
	}
	return fset
}

func TestRunAppliesScopeAndFillsPathAndSorts(t *testing.T) {
	hit := &fakeChecker{match: func(f *scan.File) []rules.Match {
		return []rules.Match{{Line: 1, Message: "hit"}}
	}}
	r := mustRule(t, "hit-go-files", finding.SeverityError, []string{"**/*.go"}, hit)
	fset := memFileSet(map[string]string{
		"z.go":     "x",
		"a.go":     "x",
		"skip.txt": "x",
	})
	got, err := engine.Run([]*config.Rule{r}, fset, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("findings: %+v", got)
	}
	if got[0].Path != "a.go" || got[1].Path != "z.go" {
		t.Fatalf("not sorted / path not filled: %+v", got)
	}
	if got[0].RuleID != "hit-go-files" || got[0].Severity != finding.SeverityError {
		t.Fatalf("envelope not applied: %+v", got[0])
	}
}

func TestRunCollectsFinalizerMatchesWithEmptyPath(t *testing.T) {
	fin := &fakeFinalizer{}
	fin.final = []rules.Match{{Message: "scope-level failure"}}
	r := mustRule(t, "exists-rule", finding.SeverityError, []string{"**"}, fin)
	got, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.txt": "x"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "" || got[0].Message != "scope-level failure" {
		t.Fatalf("finalizer findings: %+v", got)
	}
}

func TestRunReportsCheckerErrorNamingRule(t *testing.T) {
	bad := &fakeChecker{err: errFake}
	r := mustRule(t, "erroring-rule", finding.SeverityError, []string{"**"}, bad)
	_, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.txt": "x"}), 2)
	if err == nil || !strings.Contains(err.Error(), "erroring-rule") {
		t.Fatalf("checker error not surfaced with rule id: %v", err)
	}
}

func TestRunRecoversPanicsAsErrors(t *testing.T) {
	r := mustRule(t, "panicking-rule", finding.SeverityError, []string{"**"}, &fakeChecker{panics: true})
	_, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.txt": "x"}), 2)
	if err == nil || !strings.Contains(err.Error(), "panicking-rule") {
		t.Fatalf("panic not converted to error: %v", err)
	}
}

// fakeErrFinalizer is a whole-run checker (no per-file work) whose FinalizeErr
// may return an error — modelling command/git-diff execution failures.
type fakeErrFinalizer struct {
	final []rules.Match
	err   error
}

func (*fakeErrFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (c *fakeErrFinalizer) FinalizeErr(rules.FinalizeContext) ([]rules.Match, error) {
	return c.final, c.err
}

func TestRunSurfacesErrFinalizerErrorAsEngineError(t *testing.T) {
	r := mustRule(t, "cmd-rule", finding.SeverityError, []string{"**"}, &fakeErrFinalizer{err: errFake})
	_, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.txt": "x"}), 1)
	if err == nil || !strings.Contains(err.Error(), "cmd-rule") {
		t.Fatalf("ErrFinalizer error not surfaced with rule id: %v", err)
	}
}

func TestRunCollectsErrFinalizerMatches(t *testing.T) {
	fin := &fakeErrFinalizer{final: []rules.Match{{Message: "tool reported a violation"}}}
	r := mustRule(t, "cmd-ok", finding.SeverityError, []string{"**"}, fin)
	got, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.txt": "x"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "" || got[0].Message != "tool reported a violation" {
		t.Fatalf("ErrFinalizer findings: %+v", got)
	}
}

var errFake = &fakeError{}

type fakeError struct{}

func (*fakeError) Error() string { return "fake failure" }
