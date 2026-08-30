// census_symlink_test.go — #143's symlink census at the RENDERING layer (#296).
//
// PR #235 shipped two halves. internal/scan RECORDS the skip
// (Ignored{By: SourceSymlink}) and internal/meta RENDERS it as the census's
// `scan: symlink not followed:` line. The recording half is pinned by
// scan's TestNonSourceSymlinkToAFileIsCensused / TestOrdinaryFileIsNotCensused
// AsASkip; the rendering half — the change that PR actually shipped into this
// package — was pinned by nothing. Neutering the filter at census.go's loop so
// no link line is ever appended left the entire module green, and the binary
// built from that tree printed `escape hatches: none` over a tree whose only
// in-scope path is a symlink: #143's operator-facing symptom, restored in
// silence.
//
// That silence is the whole point of the line. An operator whose rule scopes
// `**/*.yaml` over a `config.yaml` symlink is told by empty-scope that their
// glob matched nothing, which points at the rule; the truth is that the walk
// declined to look.
//
// The two tests here are a differential over the same output: one tree where
// the walk declined to look, one where the operator declared the path away
// themselves. Each kills a mutation the other survives — neutering the filter
// so no line is ever emitted, and dropping the filter so every ignored record
// is called a skipped symlink.
package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// censusSymlinkRepo is the #143 reproduction as a lintable tree: one rule
// scoping `**/*.yaml` — an extension absent from scan.go's sourceExts, so the
// walk skips rather than refuses the link — with the fixtures lint needs to
// reach a verdict at all, so the census block under test is not competing with
// a fixture-coverage failure for the reader's attention.
func censusSymlinkRepo() map[string]string {
	return map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost-yaml\n" +
			"    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.yaml']}\n" +
			"    params: {pattern: SECRET_KEY}\n",
		".formwork/fixtures/no-ghost-yaml/fire-1/f.yaml": "SECRET_KEY want: no-ghost-yaml\n",
		".formwork/fixtures/no-ghost-yaml/pass-1/f.yaml": "clean\n",
	}
}

// TestCensusRendersTheSymlinkTheWalkDeclinedToFollow is the rendering half of
// #143, at the layer that renders it. The link's target must live OUTSIDE the
// tree: parked inside, the walk enumerates the target by its own name and the
// operator loses nothing by the skip, which is a different case with a
// different answer.
func TestCensusRendersTheSymlinkTheWalkDeclinedToFollow(t *testing.T) {
	root := writeRepo(t, censusSymlinkRepo())

	outside := t.TempDir()
	target := filepath.Join(outside, "config.yaml")
	if err := os.WriteFile(target, []byte("SECRET_KEY: hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// `config.yaml` is neither source-named (no sourceExts entry for .yaml) nor
	// a directory, so the walk skips it and censuses it rather than refusing
	// the scan — the third option #143 chose for this shape.
	link := filepath.Join(root, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	_, out := lintRoot(t, root)

	want := "scan: symlink not followed: config.yaml"
	if !strings.Contains(out, want) {
		t.Fatalf("the census must name the path the walk declined to follow; missing %q in:\n%s", want, out)
	}
	// The reason travels with the path or the line is a bare fact an operator
	// cannot act on — it has to say that nothing UNDER the link was scanned
	// either, which is the coverage claim being withdrawn.
	if !strings.Contains(out, "nothing under it scanned") {
		t.Fatalf("the census line must say what went unscanned, not merely that a link exists:\n%s", out)
	}
	// The affirmative "none" and the enumeration are mutually exclusive by
	// construction, and asserting both directions is what makes this a
	// differential rather than a substring hunt: a rendering that appends the
	// line but leaves the header reading `none` would still tell the operator
	// there are no escape hatches.
	if strings.Contains(out, "escape hatches: none") {
		t.Fatalf("a tree with a censused symlink must not claim it has no escape hatches:\n%s", out)
	}
}

// TestCensusDoesNotCallADeclaredIgnoreASkippedSymlink is the control, and it
// is the half that makes the pair a differential rather than a substring hunt.
// The census keys on WHAT THE WALK RECORDED, not on what a path's name looks
// like: a `config.yaml` the operator declared away in scan.ignore is a channel
// the operator can read in their own config, and reporting it as a link the
// walk declined to follow would attribute a skip they chose to a mechanism they
// did not — the same misattribution, pointed the other way, that the line under
// test exists to end.
//
// Without this arm the rendering may append a line for EVERY ignored record and
// still satisfy the test above.
func TestCensusDoesNotCallADeclaredIgnoreASkippedSymlink(t *testing.T) {
	files := censusSymlinkRepo()
	files[".formwork/formwork.yaml"] = "version: 1\nscan:\n  ignore:\n    - glob: 'vendored/**'\n      reason: third-party, not ours to fix\n"
	// A REGULAR file, in the shape the symlink case takes — same basename, same
	// extension, same rule scoping it — removed from the walk by a DECLARED
	// channel rather than by the walk declining to look.
	files["vendored/config.yaml"] = "clean: true\n"
	files["notes.txt"] = "in scope\n"
	// A git index, because scan.ignore is verifiable only against one (#90):
	// nothing under the glob is tracked, so scan-ignore-tracked passes and the
	// census block below is the only thing this test is reading.
	_, out := lintTracked(t, files, "notes.txt")

	if strings.Contains(out, "symlink not followed") {
		t.Fatalf("a declared scan.ignore match is not a symlink the walk declined to follow:\n%s", out)
	}
	// And it must still be disclosed — through its own channel, which is the
	// line that names the glob the operator wrote.
	if !strings.Contains(out, "scan.ignore: vendored/** —") {
		t.Fatalf("the declared channel must still be enumerated, by its own name:\n%s", out)
	}
}
