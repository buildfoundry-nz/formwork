package corpus_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/corpus"
)

// The package is imported as corpus_test — an EXTERNAL test package. That is
// the assertion, not a style choice: it can only see the exported surface, so
// if a symbol an analyser needs is missing, this file stops compiling.

// TestEveryBuiltinRuleTypeIsRegistered is what makes importing this package
// different from importing the pieces. An analyser that misses one registration
// does not fail — it silently measures a corpus the engine would have judged
// differently, because an unregistered type resolves to nothing.
//
// The list is spelled out rather than derived from the registry, so ADDING a
// rule type to the engine without exposing it here fails this test. Deriving it
// would make the test agree with whatever the code does.
func TestEveryBuiltinRuleTypeIsRegistered(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"baseline", "binary-content", "command", "doc-path-exists", "file-naming",
		"file-size", "forbidden-pattern", "git-diff", "ordering",
		"pair-consistency", "pattern-count", "required-pattern", "set-relation",
		// The NAME-anchored families. Each package registers several type
		// names, and a census that resolves only the family it happens to know
		// mis-classifies the rest as unknown types.
		"dart/gate-reads-are-listened", "dart/method-delegates",
		"dart/numeric-field-validated", "dart/ref-after-await",
		"go/call-confined-to-func-name", "go/call-order-in-func",
		"go/func-line-budget", "go/guard-precedes-call",
		"go/per-func-count-relation",
		"sql/locking-select-order", "sql/locking-target", "sql/parses",
		"sql/statement-predicate",
	} {
		if _, ok := corpus.Lookup(name); !ok {
			t.Errorf("rule type %q is not registered — an analyser importing this "+
				"package would measure a corpus the engine judges differently", name)
		}
	}
}

// TestLookupRejectsAnUnknownType pins the other direction: because this package
// registers every built-in, a miss means the name is genuinely unknown rather
// than merely unimported. Without that, "not found" is ambiguous and an
// analyser cannot tell a typo from a missing import.
func TestLookupRejectsAnUnknownType(t *testing.T) {
	t.Parallel()
	if _, ok := corpus.Lookup("no-such-rule-type"); ok {
		t.Fatal("an unknown rule type resolved")
	}
}

// TestWalkAndRunMeasureWithTheEnginesOwnScanner is the reason the surface
// exists at all: an analyser must measure scope with the engine's scanner, not
// a re-implementation. Re-implementing doublestar is what produced the false
// "133 of 200 rules match zero files".
func TestWalkAndRunMeasureWithTheEnginesOwnScanner(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, "kept/subject.txt", "SENTINEL here\n")
	write(t, root, "kept/clean.txt", "nothing to see\n")

	fset, err := corpus.Walk(root)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(fset.Files) != 2 {
		t.Fatalf("scanned %d file(s), want 2", len(fset.Files))
	}

	rule := loadOneRule(t, root)
	fds, err := corpus.Run([]*corpus.Rule{rule}, fset, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	live := corpus.Unsuppressed(fds)
	if len(live) != 1 {
		t.Fatalf("got %d unsuppressed finding(s), want 1 — the rule must fire on "+
			"exactly the planted subject", len(live))
	}
	if !strings.Contains(live[0].Path, "subject.txt") {
		t.Fatalf("finding on %q, want the planted subject", live[0].Path)
	}
}

// TestUnderBuiltinSkipIsReachable pins the one an analyser forgets. Without it
// a census counts files the gate can never see and reports a rule as covering
// them.
func TestUnderBuiltinSkipIsReachable(t *testing.T) {
	t.Parallel()
	if !corpus.UnderBuiltinSkip(filepath.Join(".git", "config")) {
		t.Error(".git content is not reported as skipped")
	}
	if corpus.UnderBuiltinSkip(filepath.Join("lib", "main.dart")) {
		t.Error("ordinary source is reported as skipped")
	}
}

// TestNewMemFileNeedsNoDisk keeps the probing path available: an analyser that
// asks "would this rule fire on this content" should not have to write a file.
func TestNewMemFileNeedsNoDisk(t *testing.T) {
	t.Parallel()
	f := corpus.NewMemFile("probe.txt", []byte("SENTINEL\n"))
	if f == nil || f.Path() != "probe.txt" {
		t.Fatalf("NewMemFile did not produce the probe file")
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// loadOneRule writes a minimal corpus and loads it through the public Load, so
// the test exercises the same path an analyser would.
func loadOneRule(t *testing.T, root string) *corpus.Rule {
	t.Helper()
	write(t, root, ".formwork/formwork.yaml", "version: 1\n")
	write(t, root, ".formwork/rules/probe.yaml", `rules:
- id: probe-sentinel
  type: forbidden-pattern
  severity: error
  scope:
    include: ["**/*.txt"]
  params:
    pattern: 'SENTINEL'
  cure: remove it
  tags: [always]
`)
	cfg, err := corpus.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("loaded %d rule(s), want 1", len(cfg.Rules))
	}
	return cfg.Rules[0]
}
