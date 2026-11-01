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

// matchLineOne is a checker that reports line 1 of every file it sees.
func matchLineOne() *fakeChecker {
	return &fakeChecker{match: func(f *scan.File) []rules.Match {
		return []rules.Match{{Line: 1, Message: "hit"}}
	}}
}

func TestRunPassesPreprocessVariantToChecker(t *testing.T) {
	var seen string
	c := &fakeChecker{match: func(f *scan.File) []rules.Match {
		b, err := f.Content()
		if err != nil {
			t.Error(err)
		}
		seen = string(b)
		return nil
	}}
	r := mustRule(t, "wants-variant", finding.SeverityError, []string{"**"}, c)
	r.Preprocess = "decomment-go"
	fset := memFileSet(map[string]string{"a.go": "code() // secret\n"})
	if _, err := engine.Run([]*config.Rule{r}, fset, 1); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seen, "secret") {
		t.Fatalf("checker saw raw content: %q", seen)
	}
	if !strings.Contains(seen, "code()") {
		t.Fatalf("checker saw unexpected content: %q", seen)
	}
}

func TestRunUnknownPreprocessorIsEngineError(t *testing.T) {
	r := mustRule(t, "bad-variant", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Preprocess = "no-such"
	_, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.go": "x\n"}), 1)
	if err == nil || !strings.Contains(err.Error(), "no-such") {
		t.Fatalf("unknown preprocessor not an error: %v", err)
	}
}

// No comment exempts a finding. A reasoned `formwork:allow` on the violating
// line was a suppression channel; it is now prose the engine never reads, so
// the finding stands.
func TestRunFormworkAllowCommentExemptsNothing(t *testing.T) {
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	fset := memFileSet(map[string]string{
		"a.go": "bad // formwork:allow no-hit grandfathered until v2\n",
	})
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "a.go" || got[0].Line != 1 {
		t.Fatalf("findings = %+v, want the one finding on a.go:1", got)
	}
}
