package main

import (
	"strings"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestBuildScopesWithBudgetMatchesIndex pins correctness (#12419): the
// budget-enforcing scope builder returns exactly what buildScopeIndex
// computes when the phase lands inside the budget.
func TestBuildScopesWithBudgetMatchesIndex(t *testing.T) {
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
	got, err := buildScopesWithBudget(rules, files, time.Minute)
	if err != nil {
		t.Fatalf("buildScopesWithBudget: unexpected budget error: %v", err)
	}
	want := buildScopeIndex(rules, files)
	if len(got) != len(want) {
		t.Fatalf("got %d scoped rules, want %d", len(got), len(want))
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

// TestBuildScopesWithBudgetEnforcesWall is the #12419 lockdown: when the
// O(rules×files) scope-membership phase exceeds the wall budget the builder
// must return an error naming the budget, so the census FAILs the
// Architecture Guardrails run instead of silently re-admitting the serial
// path. The budget is negative rather than sub-nanosecond (#13726): the
// builder compares MEASURED elapsed time, and a budget below the platform's
// timer granularity can never be exceeded. A Windows dev box ticks at ~514µs
// (measured 2026-08-14: 99,998 of 100,000 back-to-back time.Since() samples
// read exactly 0), so this one-rule/one-file scan lands inside a single tick,
// the old 1ns budget read as not-exceeded, and the arm was red on an
// untouched tree there while green on the finer-grained CI clock. An
// already-spent budget is over on every clock, because elapsed is never
// negative.
func TestBuildScopesWithBudgetEnforcesWall(t *testing.T) {
	rules := []*config.Rule{
		mustRule(t, "go-rule", []string{"**/*.go"}, nil),
	}
	files := []*scan.File{
		scan.NewMemFile("a/b.go", []byte("x\n")),
	}
	const exhaustedBudget = -1 * time.Second
	_, err := buildScopesWithBudget(rules, files, exhaustedBudget)
	if err == nil {
		t.Fatal("buildScopesWithBudget with an already-exhausted budget returned nil error — the wall budget is not enforced")
	}
	if !strings.Contains(err.Error(), "wall budget") {
		t.Fatalf("error %q does not name the wall budget", err)
	}
}
