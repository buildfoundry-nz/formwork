package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestBuildScopeIndexMatchesSerial pins correctness: every rule's in-scope
// set equals the naive nested-loop baseline.
func TestBuildScopeIndexMatchesSerial(t *testing.T) {
	rules := []*config.Rule{
		mustRule(t, "go-rule", []string{"**/*.go"}, nil),
		mustRule(t, "txt-rule", []string{"**/*.txt"}, nil),
		mustRule(t, "narrow", []string{"a/b.go"}, nil),
	}
	files := []*scan.File{
		scan.NewMemFile("a/b.go", []byte("x\n")),
		scan.NewMemFile("a/c.txt", []byte("x\n")),
		scan.NewMemFile("z.md", []byte("x\n")),
	}
	got := buildScopeIndex(rules, files)
	want := serialScopes(rules, files)
	for id, w := range want {
		g := got[id]
		if len(g) != len(w) {
			t.Fatalf("rule %s: got %d files, want %d", id, len(g), len(w))
		}
		for i := range w {
			if g[i].Path() != w[i].Path() {
				t.Fatalf("rule %s[%d]: got %q want %q", id, i, g[i].Path(), w[i].Path())
			}
		}
	}
}

// TestCensusScopeIndexComplexity is the #12419 lockdown: a synthetic corpus
// of R=200 rules × F=5_000 files must finish under a wall budget the naive
// serial path struggles with on a loaded CI host. Parallel index is the
// mechanical ceiling.
func TestCensusScopeIndexComplexity(t *testing.T) {
	const R, F = 200, 5000
	rules := make([]*config.Rule, 0, R)
	for i := 0; i < R; i++ {
		// Mix exact and glob includes so Applies does real work.
		inc := []string{fmt.Sprintf("pkg%03d/**/*.go", i%50), "**/*.md"}
		rules = append(rules, mustRule(t, fmt.Sprintf("r%03d", i), inc, nil))
	}
	files := make([]*scan.File, 0, F)
	for i := 0; i < F; i++ {
		p := fmt.Sprintf("pkg%03d/f%04d.go", i%50, i)
		if i%17 == 0 {
			p = fmt.Sprintf("docs/d%04d.md", i)
		}
		files = append(files, scan.NewMemFile(p, []byte("x\n")))
	}
	start := time.Now()
	scopes := buildScopeIndex(rules, files)
	elapsed := time.Since(start)
	if len(scopes) != R {
		t.Fatalf("scopes=%d want %d", len(scopes), R)
	}
	// 2s is generous for parallel index; a reintroduction of pure serial
	// O(R×F×G) on a cold host still clears it — the pin is "does not hang".
	if elapsed > 2*time.Second {
		t.Fatalf("buildScopeIndex took %s for R=%d F=%d — over the 2s wall budget (#12419)", elapsed, R, F)
	}
	t.Logf("buildScopeIndex R=%d F=%d in %s", R, F, elapsed)
}

func serialScopes(rules []*config.Rule, files []*scan.File) map[string][]*scan.File {
	scopes := make(map[string][]*scan.File, len(rules))
	for _, r := range rules {
		scopes[r.ID] = filesInScope(r, files)
	}
	return scopes
}

// TestBuildScopeIndexMatchesSerialTrickyGlobs extends the equivalence pin
// to the glob shapes and input orderings a candidate-narrowing index must
// not disturb: brace alternates, character classes, exact paths,
// overlapping literal prefixes, globstar-led patterns, and file input
// that is NOT sorted (a rule's result must stay in the walk's original
// order). Passes against both the full-scan index and the bucketed one —
// it is the contract the bucketed rewrite is held to.
func TestBuildScopeIndexMatchesSerialTrickyGlobs(t *testing.T) {
	rules := []*config.Rule{
		mustRule(t, "brace", []string{"{a,z}/*.go"}, nil),
		mustRule(t, "charclass", []string{"a/[bc].go"}, nil),
		mustRule(t, "exact", []string{"a/b.go"}, nil),
		mustRule(t, "globstar-led", []string{"**/*.txt"}, nil),
		mustRule(t, "overlap", []string{"a/**/*.go", "a/b.go"}, nil),
		mustRule(t, "mixed-prefix", []string{"a/**", "z/*.md"}, nil),
		mustRule(t, "with-exclude", []string{"a/**"}, []string{"a/sub/**"}),
	}
	files := []*scan.File{
		// Deliberately unsorted: the index must still emit each rule's
		// subset in this original order.
		scan.NewMemFile("z/q.md", []byte("x\n")),
		scan.NewMemFile("a/b.go", []byte("x\n")),
		scan.NewMemFile("z/t.txt", []byte("x\n")),
		scan.NewMemFile("a/c.go", []byte("x\n")),
		scan.NewMemFile("a/sub/deep.go", []byte("x\n")),
		scan.NewMemFile("z/one.go", []byte("x\n")),
		scan.NewMemFile("m.md", []byte("x\n")),
		scan.NewMemFile("a/note.txt", []byte("x\n")),
	}
	got := buildScopeIndex(rules, files)
	want := serialScopes(rules, files)
	if len(got) != len(want) {
		t.Fatalf("scope map size %d, want %d", len(got), len(want))
	}
	for id, w := range want {
		g := got[id]
		if len(g) != len(w) {
			t.Fatalf("rule %s: got %d files, want %d", id, len(g), len(w))
		}
		for i := range w {
			if g[i].Path() != w[i].Path() {
				t.Fatalf("rule %s[%d]: got %q want %q", id, i, g[i].Path(), w[i].Path())
			}
		}
	}
}

func mustRule(t *testing.T, id string, include, exclude []string) *config.Rule {
	t.Helper()
	r, err := config.New(id, "forbidden-pattern", finding.SeverityError, "cure", include, exclude, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
