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

func TestRunMarkerSuppressesSameLineWithReason(t *testing.T) {
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Marker = true
	fset := memFileSet(map[string]string{
		"a.go": "bad // formwork:allow no-hit grandfathered until v2\n",
	})
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Suppressed || got[0].SuppressedBy != "marker" {
		t.Fatalf("findings = %+v", got)
	}
}

func TestRunMarkerRequiresReasonAndOptIn(t *testing.T) {
	// Reasonless marker: rule opted in, but no reason after the id.
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Marker = true
	got, err := engine.Run([]*config.Rule{r},
		memFileSet(map[string]string{"a.go": "bad // formwork:allow no-hit\n"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Suppressed {
		t.Fatalf("reasonless marker suppressed: %+v", got)
	}

	// Valid marker text, but the rule did not opt in (Marker false).
	r2 := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	got, err = engine.Run([]*config.Rule{r2},
		memFileSet(map[string]string{"a.go": "bad // formwork:allow no-hit a reason\n"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Suppressed {
		t.Fatalf("marker honored without opt-in: %+v", got)
	}

	// Marker names a different rule id.
	r3 := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r3.Marker = true
	got, err = engine.Run([]*config.Rule{r3},
		memFileSet(map[string]string{"a.go": "bad // formwork:allow other-rule a reason\n"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Suppressed {
		t.Fatalf("marker for other rule suppressed: %+v", got)
	}
}

func TestRunMarkerScansRawFileEvenWithPreprocess(t *testing.T) {
	// The marker lives in a comment; decomment-go erases it from the variant.
	// Suppression must still work because marker scanning reads the raw file.
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Marker = true
	r.Preprocess = "decomment-go"
	fset := memFileSet(map[string]string{
		"a.go": "bad() // formwork:allow no-hit legacy call site\n",
	})
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Suppressed {
		t.Fatalf("marker not honored through preprocess: %+v", got)
	}
}

func TestRunMarkerCommentCloserIsNotAReason(t *testing.T) {
	// A trailing block-comment or HTML-comment closer is not a real reason;
	// suppression must not fire (marker package finding B1).
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Marker = true
	fset := memFileSet(map[string]string{
		"a.go": "bad /* formwork:allow no-hit */\n",
	})
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Suppressed {
		t.Fatalf("comment-closer-only marker wrongly suppressed: %+v", got)
	}
}

func TestRunMarkerPrefixCollisionRuleIDIsPrefixOfMarkerID(t *testing.T) {
	// Rule "no" must not be suppressed by a marker naming "no-hit".
	r := mustRule(t, "no", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Marker = true
	fset := memFileSet(map[string]string{
		"a.go": "bad // formwork:allow no-hit a real reason\n",
	})
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Suppressed {
		t.Fatalf("prefix-collision (rule id prefix of marker id) wrongly suppressed: %+v", got)
	}
}

func TestRunMarkerPrefixCollisionMarkerIDIsPrefixOfRuleID(t *testing.T) {
	// Rule "no-hit" must not be suppressed by a marker naming just "no".
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Marker = true
	fset := memFileSet(map[string]string{
		"a.go": "bad // formwork:allow no a real reason\n",
	})
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Suppressed {
		t.Fatalf("prefix-collision (marker id prefix of rule id) wrongly suppressed: %+v", got)
	}
}

func TestRunAllowlistSuppressesExactPath(t *testing.T) {
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Allowlist = &config.Allowlist{
		File:    "allowlists/legacy.txt",
		Entries: []config.AllowlistEntry{{Path: "old.go", Line: 3}},
	}
	fset := memFileSet(map[string]string{"old.go": "x\n", "new.go": "x\n"})
	got, err := engine.Run([]*config.Rule{r}, fset, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("findings = %+v", got)
	}
	// Findings are sorted by path: new.go then old.go.
	if got[0].Path != "new.go" || got[0].Suppressed {
		t.Fatalf("new.go wrongly suppressed: %+v", got[0])
	}
	if got[1].Path != "old.go" || !got[1].Suppressed ||
		got[1].SuppressedBy != "allowlist:allowlists/legacy.txt:3" {
		t.Fatalf("old.go not suppressed by allowlist: %+v", got[1])
	}
}

func TestRunScopeLevelFindingsAreNotExemptable(t *testing.T) {
	fin := &fakeFinalizer{}
	fin.final = []rules.Match{{Message: "scope-level"}}
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, fin)
	r.Marker = true
	r.Allowlist = &config.Allowlist{File: "a.txt", Entries: []config.AllowlistEntry{{Path: "", Line: 1}}}
	got, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"a.go": "x\n"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Suppressed {
		t.Fatalf("scope-level finding suppressed: %+v", got)
	}
}

func TestRunAllowlistSuppressesFinalizerFileFinding(t *testing.T) {
	fin := &fakeFinalizer{}
	fin.final = []rules.Match{{Path: "old.go", Message: "file-level"}}
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, fin)
	r.Allowlist = &config.Allowlist{
		File:    "allowlists/legacy.txt",
		Entries: []config.AllowlistEntry{{Path: "old.go", Line: 1}},
	}
	got, err := engine.Run([]*config.Rule{r}, memFileSet(map[string]string{"old.go": "x\n"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Suppressed {
		t.Fatalf("finalizer file-level finding not suppressed: %+v", got)
	}
}
