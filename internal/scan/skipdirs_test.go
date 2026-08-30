package scan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// #268. The two members of the built-in skip set are not the same kind of
// thing, and pruning them by BASENAME AT ANY DEPTH conflated them.
//
// `.git` is VCS internals wherever it appears — a submodule's, a nested
// clone's, a linked worktree's — so any depth is right for it.
//
// `.formwork` at the walk ROOT is the engine's own config and fixtures, and
// skipping it is load-bearing: `.formwork/fixtures` holds deliberately-broken
// fire fixtures whose whole job is to contain violations, so scanning them
// would make every rule fail on its own test data. A `.formwork` DEEPER in the
// tree is not that. It is somebody else's repository sitting inside this one —
// a ported corpus under examples/, a vendored subproject — and it is ordinary
// governed content, because the run that owns those fixtures is the one rooted
// AT that directory (`formwork test -C examples/<corpus>`), where they are at
// the root again and skipped again.
//
// Conflating the two blinded this repository's permanent genericisation gate —
// the rule that fails a real product, domain or vendor token reintroduced into
// a ported corpus — to 2,474 of the 2,492 tracked files under examples/: every
// ported corpus lives in examples/<corpus>/.formwork/, so the rule scoped
// `examples/**` saw 18 files and the porting surface it exists to police —
// 2,399 files in examples/palletra-port-full — was structurally unreachable.
func TestBuiltinSkipPrunesFormworkAtTheWalkRootOnly(t *testing.T) {
	for _, tc := range []struct {
		path     string
		underAny bool // UnderBuiltinSkip: any component, leaf included
		underDir bool // UnderBuiltinSkipDir: ancestors only
	}{
		// The engine's own — first segment, still invisible.
		{".formwork/rules/r.yaml", true, true},
		{".formwork/fixtures/no-ghost/fire-1/a.go", true, true},
		{".formwork", true, false},

		// A corpus's own — ordinary content now.
		{"examples/palletra-port-full/.formwork/rules/r.yaml", false, false},
		{"examples/palletra-port-full/.formwork/fixtures/x/fire-1/a.go", false, false},
		{"sub/.formwork/formwork.yaml", false, false},
		{"sub/.formwork", false, false},

		// .git keeps ANY depth: it is VCS internals wherever it sits.
		{".git/config", true, true},
		{"a/b/.git/hooks/pre-commit", true, true},
		{"vendor/dep/.git/objects/aa/bb", true, true},
		// ...but only as a DIRECTORY component. A regular FILE named .git is
		// the linked-worktree gitdir pointer, and the walk scans it (#158).
		{"sub/.git", true, false},

		// Neither name, no dot: never in the set.
		{"formwork/rules/r.yaml", false, false},
		{"a/.github/workflows/ci.yml", false, false},
		{"src/main.go", false, false},
	} {
		if got := scan.UnderBuiltinSkip(tc.path); got != tc.underAny {
			t.Errorf("UnderBuiltinSkip(%q) = %v, want %v", tc.path, got, tc.underAny)
		}
		if got := scan.UnderBuiltinSkipDir(tc.path); got != tc.underDir {
			t.Errorf("UnderBuiltinSkipDir(%q) = %v, want %v", tc.path, got, tc.underDir)
		}
	}
}

// The walk is the authority the predicates above only describe, so it gets its
// own row: a nested .formwork tree must be ENUMERATED, and the root one must
// still not be. Both halves in one tree, so neither can be satisfied by a
// change that simply stops pruning.
func TestWalkEnumeratesANestedFormworkButNotTheRootOne(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".formwork", "rules", "own.yaml"), "rules: []\n")
	writeFile(t, filepath.Join(root, ".formwork", "fixtures", "r", "fire-1", "bad.go"), "package p\n")
	writeFile(t, filepath.Join(root, "examples", "corpus", ".formwork", "rules", "ported.yaml"), "rules: []\n")
	writeFile(t, filepath.Join(root, "examples", "corpus", ".formwork", "fixtures", "r", "fire-1", "bad.go"), "package p\n")
	writeFile(t, filepath.Join(root, "examples", "corpus", "src", "a.go"), "package a\n")
	writeFile(t, filepath.Join(root, "sub", ".git", "config"), "[core]\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")

	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	want := []string{
		"examples/corpus/.formwork/fixtures/r/fire-1/bad.go",
		"examples/corpus/.formwork/rules/ported.yaml",
		"examples/corpus/src/a.go",
		"main.go",
	}
	if got := paths(fset); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

// NotScannedBy is the walk's attribution twin — lint's census, `rules-for`'s
// annotation and --staged's accounting all read it — so a depth rule the walk
// applies and this does not is the two disagreeing about whether a path was
// ever looked at.
func TestNotScannedByFollowsTheSameDepthRule(t *testing.T) {
	if v := scan.NotScannedBy("examples/corpus/.formwork/rules/r.yaml", scan.Opts{}); v.Hidden() {
		t.Errorf("a corpus's own .formwork is scanned content: %+v", v)
	}
	if v := scan.NotScannedBy(".formwork/rules/r.yaml", scan.Opts{}); !v.Hidden() || !v.Builtin {
		t.Errorf("the engine's own .formwork must stay builtin-hidden: %+v", v)
	}
	if v := scan.NotScannedBy("vendor/dep/.git/config", scan.Opts{}); !v.Hidden() || !v.Builtin {
		t.Errorf(".git stays builtin-hidden at any depth: %+v", v)
	}
	// The operator channel still wins when it prunes ABOVE the nested tree —
	// unchanged, and the shallowest-trigger order is what decides it.
	v := scan.NotScannedBy("examples/corpus/.formwork/rules/r.yaml", scan.Opts{Ignore: []string{"examples/**"}})
	if !v.Hidden() || v.Builtin || v.Glob != "examples/**" {
		t.Errorf("an operator glob above the corpus must own the attribution: %+v", v)
	}
}

// The subtree-hiding symlink refusal (#143 row 2) consults the same skip set,
// and it must move with it. A ROOT `.formwork` symlink is the shared-rule-set
// monorepo the engine invites, and skipping it silently loses nothing the walk
// was ever going to enumerate. A NESTED one now hides real governed content
// behind a link the walk will not follow, which is exactly the state the
// refusal exists to make loud — leaving it skipped would trade one silent
// blindness for another.
func TestNestedFormworkSymlinkHidingASubtreeIsRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "shared", "rules", "r.yaml"), "rules: []\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	if err := os.MkdirAll(filepath.Join(root, "examples", "corpus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "shared"), filepath.Join(root, "examples", "corpus", ".formwork")); err != nil {
		t.Fatal(err)
	}

	_, err := scan.Walk(root)
	if err == nil {
		t.Fatal("a nested .formwork symlink hides scannable content — the walk must refuse it, not skip it")
	}
	if !strings.Contains(err.Error(), "examples/corpus/.formwork") {
		t.Fatalf("the refusal must name the path: %v", err)
	}
}

// The other half, kept adjacent so the pair cannot drift apart: the ROOT
// spelling stays skipped, at exit 0, with the real tree still enumerated.
func TestRootFormworkSymlinkIsStillSkipped(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "shared", "rules", "r.yaml"), "rules: []\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	if err := os.Symlink(filepath.Join(outside, "shared"), filepath.Join(root, ".formwork")); err != nil {
		t.Fatal(err)
	}

	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatalf("a root .formwork symlink is the shared-rule-set case; got: %v", err)
	}
	if got := paths(fset); !reflect.DeepEqual(got, []string{"main.go"}) {
		t.Fatalf("paths = %v, want [main.go]", got)
	}
}
