package vcs_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// #99: THE RANGE STRING IS TOKENIZED, NOT WHITESPACE-SPLIT.
//
// `strings.Fields` cannot express a pathspec whose NAME contains a space:
// `HEAD~1..HEAD -- my dir` reached git as two pathspecs, `my` and `dir`.
// Unmatched diff pathspecs are not errors — git exits 0 with empty output — so
// every per-file rule saw zero files and the gate exited 0 over an unscanned
// changeset. Each spelling below is a way to say "one pathspec, with a space in
// it"; all three must scan the directory the operator named.
func TestRangePathsScansAQuotedPathspecContainingASpace(t *testing.T) {
	dir := spacedPathspecRepo(t)
	for _, rng := range []string{
		`HEAD~1..HEAD -- 'my dir'`,
		`HEAD~1..HEAD -- "my dir"`,
		`HEAD~1..HEAD -- my\ dir`,
	} {
		t.Run(rng, func(t *testing.T) {
			got, err := vcs.RangePaths(dir, rng)
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"my dir/evil.ts"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("RangePaths(%q) = %v, want %v — an empty set here is the silent unscanned changeset #99 is about", rng, got, want)
			}
		})
	}
}

// The tokenizer is shared by every reader of a range string, which is the
// altitude point: RangeModes keys its answer by path and gitdiff rules read
// vcs.Diff, so a tokenizer applied to RangePaths alone would leave those two
// judging a different request from the one the operator made — RangeModes
// returning no mode for a path RangePaths listed, and a git-diff rule matching
// an empty diff.
func TestEveryRangeReaderSharesTheTokenizer(t *testing.T) {
	dir := spacedPathspecRepo(t)
	const rng = `HEAD~1..HEAD -- 'my dir'`

	modes, err := vcs.RangeModes(dir, rng)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := modes["my dir/evil.ts"]; !ok || got != vcs.ModeBlob {
		t.Errorf("RangeModes(%q)[%q] = %q, ok=%v, want %q", rng, "my dir/evil.ts", got, ok, vcs.ModeBlob)
	}

	diff, err := vcs.Diff(dir, rng)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "my dir/evil.ts") {
		t.Errorf("Diff(%q) does not mention the spaced pathspec's file:\n%s", rng, diff)
	}
}

// Space-free pathspecs keep meaning what they always meant: unquoted whitespace
// is still a separator, so #97's multi-pathspec tails are untouched. Without
// this the tokenizer could "fix" #99 by treating the whole tail as one
// pathspec, which would break every caller that passes two.
func TestRangePathsKeepsMultiplePathspecsSeparate(t *testing.T) {
	dir := spacedPathspecRepo(t)
	got, err := vcs.RangePaths(dir, `HEAD~1..HEAD -- 'my dir' other`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"my dir/evil.ts", "other/ok.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePaths with two pathspecs = %v, want %v", got, want)
	}
}

// A quote the operator never closed is a range formwork cannot tokenize, and
// the whole point of #99 is that a range it cannot serve must not read as "no
// changes". Refusing is exit 2 through the caller; guessing would be the silent
// empty set again.
func TestRangePathsRefusesAnUnclosedQuote(t *testing.T) {
	dir := spacedPathspecRepo(t)
	for _, rng := range []string{
		`HEAD~1..HEAD -- 'my dir`,
		`HEAD~1..HEAD -- "my dir`,
		`HEAD~1..HEAD -- my\`,
	} {
		t.Run(rng, func(t *testing.T) {
			if got, err := vcs.RangePaths(dir, rng); err == nil {
				t.Fatalf("RangePaths(%q) = %v with no error, want a refusal", rng, got)
			}
			if _, err := vcs.RangeModes(dir, rng); err == nil {
				t.Fatalf("RangeModes(%q) returned no error, want a refusal", rng)
			}
			if _, err := vcs.Diff(dir, rng); err == nil {
				t.Fatalf("Diff(%q) returned no error, want a refusal", rng)
			}
		})
	}
}

// spacedPathspecRepo builds the #99 reproduction: a base commit, then a commit
// adding a file under a directory whose name carries a space plus one under a
// space-free sibling, so a mis-split tail can be told from a correct one by
// which files come back.
func spacedPathspecRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	write(t, dir, "base.go", "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "my dir/evil.ts", "export const x = 1\n")
	write(t, dir, "other/ok.ts", "export const y = 2\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "add spaced dir")
	return dir
}
