package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// File-set mode tests (--staged / --range) that must run with the DEFAULT
// `-C .`, the way a git hook does.
//
// Split out of cli_test.go when that file crossed the repo's own 750-line hard
// cap (file-size-vendor-cap). Kept together because they share one property the
// rest of the package does not: they call t.Chdir, so the whole package must
// stay serial. See the note above runCLI in cli_test.go.

// TestCheckStagedFromRepoRootWithDefaultC pins the invocation every other
// file-set test in this package skips by always passing an explicit absolute
// -C: running from inside the repository root with the DEFAULT root of ".".
//
// That is not an exotic case — it is what a git hook does. Git runs hooks with
// the working directory set to the repository root, and the shim `formwork
// hooks install` generates passes no -C at all. So this path was every
// installed hook's path, and it was exit 2 on every commit (#142): the
// top-level guard compared an absolute resolved top against a root of "."
// and could never match.
func TestCheckStagedFromRepoRootWithDefaultC(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "staged.go"), "package a\nvar x = WIDGET\n")
	gitInit(t, root)
	gitRun(t, root, "add", ".formwork", "staged.go")

	t.Chdir(root)

	code, out, errOut := runCLI(t, "check", "--staged")
	if code == 2 {
		t.Fatalf("--staged from the repo root with the default -C must not be an engine error: %s", errOut)
	}
	if code != 1 {
		t.Fatalf("want exit 1 (staged.go violates), got %d\n%s", code, out)
	}
	if !strings.Contains(out, "staged.go") {
		t.Fatalf("--staged should have scanned staged.go:\n%s", out)
	}
}

// TestCheckStagedFromSubdirStillRefused is the guard's other half. Accepting a
// relative root must not become "accept any root": from a SUBDIRECTORY, git's
// repo-relative paths and the scan's root-relative paths disagree, the
// intersection silently matches nothing, and the gate would pass over an
// unscanned changeset. That is a fail-open, so it stays exit 2.
//
// The subdirectory carries its OWN .formwork/ — a nested project inside a
// larger repo. Without it the run dies earlier on a missing config, which is
// also exit 2 but proves nothing about this guard.
func TestCheckStagedFromSubdirStillRefused(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(sub, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(sub, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(sub, "staged.go"), "package a\nvar x = WIDGET\n")
	gitInit(t, root)
	gitRun(t, root, "add", ".")

	t.Chdir(sub)

	code, _, errOut := runCLI(t, "check", "--staged")
	if code != 2 {
		t.Fatalf("--staged from a subdirectory must fail closed (exit 2), got %d", code)
	}
	if !strings.Contains(errOut, "repository root") {
		t.Fatalf("stderr should explain the root requirement: %q", errOut)
	}
}

// TestScopeFromRepoRootWithDefaultC pins a behaviour change this branch caused
// as a side effect, which was previously untestable and therefore untested.
//
// runScope falls back to class=runtime with every language flagged changed
// whenever the git file-set lookup errors. Before the #142 fix, EnsureTopLevel
// rejected the default root of ".", so that fallback was the ONLY reachable arm
// for `formwork scope --staged` run from a repo root — the classifier was a
// constant function on its most natural invocation, and every existing scope
// test dodged that by passing an explicit absolute -C.
//
// Now it classifies for real, which is what spec §8 always specified. Pinned
// here so the change cannot drift back or move again unnoticed. The separate
// question of what an EMPTY changeset should mean (Classify returns docs, which
// is a fail-open shape for a gating consumer) is #147, not this test.
func TestScopeFromRepoRootWithDefaultC(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n    dart: ['**/*.dart']\n")
	mustWrite(t, filepath.Join(root, "docs.md"), "# notes\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")
	gitInit(t, root)
	gitRun(t, root, "add", "docs.md")

	t.Chdir(root)

	code, out, errOut := runCLI(t, "scope", "--staged")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	// A docs-only change must classify as docs with no language flagged. Before
	// the fix this said class=runtime with every flag true.
	if !strings.Contains(out, "class=docs") {
		t.Fatalf("docs-only staged change should classify as docs:\n%s", out)
	}
	for _, flag := range []string{"go_changed=false", "dart_changed=false"} {
		if !strings.Contains(out, flag) {
			t.Fatalf("want %s in a docs-only classification:\n%s", flag, out)
		}
	}
	if strings.Contains(errOut, "assuming runtime") {
		t.Fatalf("the fail-closed fallback must not fire on a healthy repo: %q", errOut)
	}

	// Staging Go source moves it to runtime, with only the matching language set.
	gitRun(t, root, "add", "a.go")
	code, out, _ = runCLI(t, "scope", "--staged")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("a staged .go file should be runtime + go_changed:\n%s", out)
	}
	if !strings.Contains(out, "dart_changed=false") {
		t.Fatalf("dart must not be flagged when no dart file changed:\n%s", out)
	}
}

// TestScopeFallsBackOnGitErrorWithDefaultC pins the other half: relaxing the
// root guard must not have cost `scope` its fail-closed arm on a REAL git
// failure. It also pins the operator-visible stderr prefix, which was asserted
// nowhere — making it a contract rather than an accident, since it is the only
// signal that a classification was assumed rather than computed.
func TestScopeFallsBackOnGitErrorWithDefaultC(t *testing.T) {
	root := t.TempDir() // deliberately NOT a git repo
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n")

	t.Chdir(root)

	code, out, errOut := runCLI(t, "scope", "--staged")
	if code != 0 {
		t.Fatalf("scope must not fail hard, exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("a git error must fall back to runtime with every flag true:\n%s", out)
	}
	if !strings.Contains(errOut, "formwork: scope: assuming runtime:") {
		t.Fatalf("the assumed classification must say so on stderr, got %q", errOut)
	}
}

// scopeEmptyPrefix is the pinned operator-visible contract for the EMPTY arm
// of scope's fail-closed fallback. It is deliberately not a prefix of, and does
// not contain, the git-error line ("formwork: scope: assuming runtime:") that
// TestScopeFallsBackOnGitErrorWithDefaultC pins: the two causes need different
// next actions from the operator — one says "your git call failed", the other
// says "git answered, and named nothing". A wrapper that logs one and not the
// other must be able to tell them apart. Text AFTER the prefix stays free to
// improve; the prefix itself is the contract.
const scopeEmptyPrefix = "formwork: scope: empty changeset — assuming runtime:"

// TestScopeEmptyChangesetIsRuntimeWithDefaultC — #147.
//
// An empty changeset classifies `docs` with every language flag false, because
// ScopeConfig.Classify is a pure function over zero paths and `docs` is the
// correct classification of nothing. That is the right answer to the wrong
// question: a consumer routing lanes off this output reads the WEAKEST class
// and skips every runtime check, at exit 0, silently. runScope cannot tell a
// genuinely-empty changeset from one emptied spuriously (#99's whitespace-split
// --range, a CI base-ref step that did not run), so it must assume the strongest.
//
// All three ways of asking must agree: --staged, --range, and no flag at all
// (which defaults to the staged set). A guard on one of them is how `check` and
// `scope` came to give opposite answers to identical input once already
// (cli.go, rangeValueUsable's doc comment).
func TestScopeEmptyChangesetIsRuntimeWithDefaultC(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n    dart: ['**/*.dart']\n")
	gitInit(t, root)
	gitRun(t, root, "add", ".formwork")
	gitRun(t, root, "commit", "-q", "-m", "init")
	// Index now equals HEAD: nothing staged, and HEAD..HEAD spans no commit.

	t.Chdir(root)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"staged", []string{"scope", "--staged"}},
		{"no flag", []string{"scope"}},
		{"range", []string{"scope", "--range", "HEAD..HEAD"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCLI(t, tc.args...)
			if code != 0 {
				t.Fatalf("scope classifies, it does not gate: exit %d\n%s\n%s", code, out, errOut)
			}
			if !strings.Contains(out, "class=runtime") {
				t.Errorf("an empty changeset must assume the strongest class:\n%s", out)
			}
			for _, flag := range []string{"go_changed=true", "dart_changed=true"} {
				if !strings.Contains(out, flag) {
					t.Errorf("want %s — an assumed classification flags every language:\n%s", flag, out)
				}
			}
			if !strings.Contains(errOut, scopeEmptyPrefix) {
				t.Errorf("the empty cause must announce itself with %q, got %q", scopeEmptyPrefix, errOut)
			}
			if strings.Contains(errOut, "formwork: scope: assuming runtime:") {
				t.Errorf("the empty arm must not borrow the git-error line: %q", errOut)
			}
		})
	}
}

// TestScopeCountsStagedDeletion — #147, the non-empty half.
//
// Deleting a Go file while adding a doc is a NON-empty changeset that still
// classified `docs` with go_changed=false, because vcs.StagedPaths filters
// --diff-filter=ACMR. That filter is right for `check`, which reads file
// contents and cannot scan a path that is gone; it is wrong for `scope`, which
// classifies a CHANGE. Deleting the only caller of a Go API is a runtime change
// by any reading, and it routed to the docs lane.
func TestScopeCountsStagedDeletion(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n    dart: ['**/*.dart']\n")
	mustWrite(t, filepath.Join(root, "src", "e.go"), "package src\n")
	gitInit(t, root)
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "init")

	mustWrite(t, filepath.Join(root, "NOTES.md"), "# notes\n")
	gitRun(t, root, "rm", "-q", filepath.Join("src", "e.go"))
	gitRun(t, root, "add", "NOTES.md")

	t.Chdir(root)

	code, out, errOut := runCLI(t, "scope", "--staged")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("a staged deletion of a .go file is a runtime change:\n%s", out)
	}
	if !strings.Contains(out, "dart_changed=false") {
		t.Fatalf("dart must not be flagged when no dart file changed:\n%s", out)
	}
	// The set is NOT empty here, so the empty arm must stay silent — otherwise
	// this would pass for the wrong reason, via the fallback rather than via a
	// deletion that was actually counted.
	if strings.Contains(errOut, scopeEmptyPrefix) {
		t.Fatalf("a deletion is a real path, not an empty changeset: %q", errOut)
	}
}

// TestScopeCountsStagedRenameSource — #147 review round, the rename half.
//
// `git diff --name-only` reports only a rename's DESTINATION, so `git mv
// src/api.go docs/api.md` reaches Classify as the single path docs/api.md and a
// renamed-away Go file leaves go_changed=false. Dropping --diff-filter=ACMR did
// not reach this: the source path is not filtered out, it is never emitted.
//
// Worse than a missing path, the answer depended on the operator's git config —
// measured, with git's default rename detection go_changed=false, and under
// `git -c diff.renames=false` the same index gave go_changed=true. --no-renames
// on scope's acquisition pins the widening spelling for identical bytes.
func TestScopeCountsStagedRenameSource(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n    dart: ['**/*.dart']\n")
	mustWrite(t, filepath.Join(root, "src", "api.go"), "package src\n")
	// docs/ must exist in the base commit: git mv refuses a destination whose
	// directory is not there, so the fixture cannot create it by the move.
	mustWrite(t, filepath.Join(root, "docs", ".keep"), "")
	gitInit(t, root)
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "init")

	gitRun(t, root, "mv", filepath.Join("src", "api.go"), filepath.Join("docs", "api.md"))

	t.Chdir(root)

	code, out, errOut := runCLI(t, "scope", "--staged")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("renaming a .go file away is a runtime change:\n%s", out)
	}
	if !strings.Contains(out, "dart_changed=false") {
		t.Fatalf("dart must not be flagged when no dart file changed:\n%s", out)
	}
	// Same guard as the deletion sibling: the set is non-empty, so passing via
	// the empty-changeset fallback would be passing for the wrong reason.
	if strings.Contains(errOut, scopeEmptyPrefix) {
		t.Fatalf("a rename names two real paths, not an empty changeset: %q", errOut)
	}
}

// TestScopeClassifiesForRealAcrossEverySelector closes the coverage gap #147
// Task 1 names: the healthy-classification tests above exercise --staged only,
// while the EMPTY-changeset test covers all three selectors. So "all three ways
// of asking agree" was pinned for the assumed arm and not for the computed one.
//
// GREEN AT BIRTH, deliberately. These pin behaviour that already works; they
// fill a coverage gap rather than drive a change, and dressing them up as RED
// would misreport what they are. Precedent:
// docs/plans/2026-08-05-scan-ignore-tracked-crosscheck.md does the same.
func TestScopeClassifiesForRealAcrossEverySelector(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n    dart: ['**/*.dart']\n")
	mustWrite(t, filepath.Join(root, "notes.md"), "# base\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-m", "base")

	t.Chdir(root)

	// A Go-only commit, read back through --range.
	mustWrite(t, filepath.Join(root, "a.go"), "package a // changed\n")
	gitRun(t, root, "commit", "-am", "go only")

	code, out, errOut := runCLI(t, "scope", "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("--range exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("--range over a go-only commit should be runtime + go_changed:\n%s", out)
	}
	if !strings.Contains(out, "dart_changed=false") {
		t.Fatalf("--range must not flag a language nothing touched:\n%s", out)
	}
	if strings.Contains(errOut, "assuming runtime") || strings.Contains(errOut, scopeEmptyPrefix) {
		t.Fatalf("a computed classification must not announce itself as assumed: %q", errOut)
	}

	// A docs-only commit, same selector. The weakest class must still be
	// reachable through --range, or the fail-closed arm is doing the work.
	mustWrite(t, filepath.Join(root, "notes.md"), "# changed\n")
	gitRun(t, root, "commit", "-am", "docs only")

	code, out, _ = runCLI(t, "scope", "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("--range exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "class=docs") {
		t.Fatalf("--range over a docs-only commit should be docs:\n%s", out)
	}
	for _, flag := range []string{"go_changed=false", "dart_changed=false"} {
		if !strings.Contains(out, flag) {
			t.Fatalf("want %s over a docs-only range:\n%s", flag, out)
		}
	}

	// No flag at all is the staged set, and must agree with --staged.
	mustWrite(t, filepath.Join(root, "b.go"), "package b\n")
	gitRun(t, root, "add", "b.go")

	_, noFlag, _ := runCLI(t, "scope")
	_, staged, _ := runCLI(t, "scope", "--staged")
	if noFlag != staged {
		t.Fatalf("no flag and --staged must classify identically:\nno flag:\n%s\n--staged:\n%s", noFlag, staged)
	}
	if !strings.Contains(noFlag, "class=runtime") || !strings.Contains(noFlag, "go_changed=true") {
		t.Fatalf("a staged .go file should be runtime + go_changed with no flag:\n%s", noFlag)
	}
}

// TestScopeFromSubdirFallsBackUnderAmbientGitEnv pins the subdirectory arm and
// the closure of #150 — which are two different refusals, not one.
//
// An earlier version of this test set GIT_DIR *and* GIT_WORK_TREE and claimed to
// pin the GIT_* scrub. It pinned nothing: GIT_WORK_TREE restores --show-prefix
// to "src/", so that pair takes the same path as no environment at all, and
// emptying scrubbedGitVars left the test green. It was a duplicate of its own
// first case wearing a second name. Caught by claim-auditor, 2026-08-19.
//
// #150's state is GIT_DIR set ALONE: measured on git 2.50.1 that empties
// --show-prefix and moves --show-toplevel to the subdirectory, so the
// subdirectory guard goes inert. It is closed not by that guard but by the
// ambient-environment refusal (#167), which is why the two cases below assert
// DIFFERENT stderr — asserting the same string for both is how the earlier
// version hid that it was exercising one code path twice.
func TestScopeFromSubdirFallsBackUnderAmbientGitEnv(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	// The config must live at the root -C names, which here is the subdirectory.
	mustWrite(t, filepath.Join(sub, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n")
	mustWrite(t, filepath.Join(sub, "a.go"), "package a\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-m", "base")
	mustWrite(t, filepath.Join(sub, "a.go"), "package a // changed\n")
	gitRun(t, root, "add", "src/a.go")

	t.Chdir(root)

	for _, tc := range []struct {
		name    string
		gitDir  string
		wantErr string
	}{
		{
			name:    "no ambient env: the subdirectory guard refuses",
			wantErr: "file-set modes require -C to be the repository root",
		},
		{
			name:   "GIT_DIR alone: the subdirectory guard is inert, the environment guard refuses",
			gitDir: "use root",
			// Deliberately NOT the subdirectory wording. Under GIT_DIR alone git
			// reports an empty prefix, so the guard above cannot fire; what keeps
			// scope fail-closed here is the scrub-is-not-inert check (#167).
			wantErr: "in the environment moves the repository",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.gitDir != "" {
				t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
			}
			code, out, errOut := runCLI(t, "scope", "-C", "src", "--staged")
			if code != 0 {
				t.Fatalf("scope classifies, it does not gate: exit %d\n%s", code, out)
			}
			if !strings.Contains(out, "class=runtime") {
				t.Fatalf("a root scope cannot trust must fall back to the strongest class:\n%s", out)
			}
			if !strings.Contains(errOut, "formwork: scope: assuming runtime:") {
				t.Fatalf("the assumed classification must say so on stderr, got %q", errOut)
			}
			if !strings.Contains(errOut, tc.wantErr) {
				t.Fatalf("want the %s refusal (%q) on stderr, got %q", tc.name, tc.wantErr, errOut)
			}
		})
	}
}

// TestCheckStagedToleratesAStagedDeletion pins the --diff-filter=ACMR that
// changesetFor's scannableOnly argument selects (#188). That filter is the only
// reason a staged DELETION never reaches refuseUnaccountedPaths: the deleted
// path is gone from the working tree, so the walk cannot produce it, and the
// accounting would call it "named by git but not present in the working tree" —
// exit 2 on every commit that removes a file, which is every installed
// pre-commit hook refusing a deletion.
//
// Nothing tested it. Before #147 the filter was written inline at the call site
// where it read as obviously deliberate; it is now a parameter one token from
// its opposite, and scope passes that opposite token two files away.
//
// The commit shape is a real one — one file deleted, one modified — and the
// modification VIOLATES, so exit 1 also proves the run reached the engine over
// the surviving path rather than passing by scanning nothing.
func TestCheckStagedToleratesAStagedDeletion(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "keep.go"), "package a\n")
	mustWrite(t, filepath.Join(root, "src", "gone.go"), "package b\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "base")

	gitRun(t, root, "rm", "-q", filepath.Join("src", "gone.go"))
	mustWrite(t, filepath.Join(root, "src", "keep.go"), "package a\nvar x = WIDGET\n")
	gitRun(t, root, "add", filepath.Join("src", "keep.go"))

	t.Chdir(root)

	code, out, errOut := runCLI(t, "check", "--staged")
	if code == 2 {
		t.Fatalf("a staged deletion must not make the run an engine error: %s", errOut)
	}
	if strings.Contains(errOut, "the scan never produced") {
		t.Fatalf("the deleted path reached the unaccounted-paths refusal:\n%s", errOut)
	}
	if code != 1 {
		t.Fatalf("want exit 1 (keep.go violates), got %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, filepath.Join("src", "keep.go")) {
		t.Fatalf("the surviving staged path should have been scanned:\n%s", out)
	}
}

// The same pin for --range, which resolves to vcs.RangePaths and its own copy of
// the filter. It is a separate mutation, not a second spelling of the one above:
// the CLI hands both modes one statuses argument, but each mode carries its own
// --diff-filter=ACMR at the vcs seam, and only the staged one is pinned there
// (TestStagedPathsListsAddedAndModifiedNotDeletedOrUnstaged has no range twin).
func TestCheckRangeToleratesADeletionCommit(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "keep.go"), "package a\n")
	mustWrite(t, filepath.Join(root, "src", "gone.go"), "package b\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "base")

	gitRun(t, root, "rm", "-q", filepath.Join("src", "gone.go"))
	mustWrite(t, filepath.Join(root, "src", "keep.go"), "package a\nvar x = WIDGET\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "delete one, change one")

	t.Chdir(root)

	code, out, errOut := runCLI(t, "check", "--range", "HEAD~1..HEAD")
	if code == 2 {
		t.Fatalf("a deletion inside the range must not make the run an engine error: %s", errOut)
	}
	if strings.Contains(errOut, "the scan never produced") {
		t.Fatalf("the deleted path reached the unaccounted-paths refusal:\n%s", errOut)
	}
	if code != 1 {
		t.Fatalf("want exit 1 (keep.go violates), got %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, filepath.Join("src", "keep.go")) {
		t.Fatalf("the surviving changed path should have been scanned:\n%s", out)
	}
}
