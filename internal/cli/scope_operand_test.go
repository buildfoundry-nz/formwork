// scope_operand_test.go — `formwork scope <path>...` classifies the paths it
// was given (#288).
//
// WHY THIS EXISTS. docs/reference.md has published `formwork scope <path>...`
// since #109, and the binary read no positional argument at all: runScope
// registered -staged/-range and never touched fs.Args(). Every documented
// invocation answered about the STAGED set instead — `scope docs/a.md`,
// `scope`, and `scope no/such/path another/bogus /etc/passwd` printed
// byte-identical output at exit 0. An operator wiring `formwork scope
// <changed-file>` into a CI router got the class of whatever happened to be
// staged, believing they had asked about the path they typed.
//
// Two compounding shapes, each pinned separately below because each is its own
// branch:
//
//   - Go's flag package stops parsing at the first non-flag argument, so the
//     documented operand form SWALLOWS a following mode flag. `scope docs/b.md
//     --range bogusref..HEAD` classified the staged set at exit 0 with nothing
//     on stderr, while `scope --range bogusref..HEAD` fail-closed loudly with
//     git's error. The documented form converted a fail-closed git error into a
//     silent answer — the #99 shape runScope's own comments exist to prevent.
//
//   - `scope /etc/passwd` answered with a confident class at exit 0, while its
//     sibling `rules-for /etc/passwd` exits 2. The Introspection section's own
//     preface promises the latter for both: "an out-of-root path is exit 2 — an
//     empty answer to a wrong question would be a guidance fail-open."
//
// Refusals assert on stdout too: an exit 2 that still printed a `class=` line
// would leave a wrapper reading stdout with an answer to a question formwork
// refused to answer.
package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// scopeOperandRepo builds a repo whose STAGED changeset classifies runtime+go,
// so that any answer derived from the staged set is distinguishable from an
// answer derived from a path operand. Nothing here is staged as docs: a test
// asserting class=docs can only be reading the operand.
func scopeOperandRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscope:\n"+
			"  docs: ['**/*.md', 'docs/**']\n"+
			"  governance: ['.formwork/**', '.github/**']\n"+
			"  languages:\n    go: ['**/*.go']\n    dart: ['**/*.dart']\n")
	mustWrite(t, filepath.Join(root, "docs", "a.md"), "# notes\n")
	mustWrite(t, filepath.Join(root, "internal", "x.go"), "package x\n")
	mustWrite(t, filepath.Join(root, "lib", "main.dart"), "void main() {}\n")
	gitInit(t, root)
	gitRun(t, root, "add", filepath.Join("internal", "x.go"))
	return root
}

func TestScopeClassifiesTheNamedPathsNotTheStagedSet(t *testing.T) {
	root := scopeOperandRepo(t)
	for _, tc := range []struct {
		name  string
		args  []string
		class string
		flags []string
	}{
		{"a docs path", []string{"docs/a.md"}, "class=docs", []string{"go_changed=false", "dart_changed=false"}},
		{"a governance path", []string{".formwork/formwork.yaml"}, "class=governance", []string{"go_changed=false"}},
		{"a language path", []string{"lib/main.dart"}, "class=runtime", []string{"dart_changed=true", "go_changed=false"}},
		// Deliberately NOT internal/x.go: that is the staged file, so this
		// case would have passed against the staged set it is meant to
		// distinguish itself from. dart_changed=true + go_changed=false is
		// reachable only by reading the operands.
		{"several paths take the strongest class", []string{"docs/a.md", "lib/main.dart"}, "class=runtime", []string{"dart_changed=true", "go_changed=false"}},
		// Guidance is asked about files not yet written — the same frame
		// rules-for keeps. A path that does not exist is a legitimate query,
		// not an error.
		{"a path not yet written", []string{"docs/unwritten.md"}, "class=docs", []string{"go_changed=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCLI(t, append([]string{"scope", "-C", root}, tc.args...)...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
			}
			if !strings.Contains(out, tc.class) {
				t.Fatalf("scope %v must classify the paths it was given, want %s:\n%s", tc.args, tc.class, out)
			}
			for _, f := range tc.flags {
				if !strings.Contains(out, f) {
					t.Errorf("want %s in the classification of %v:\n%s", f, tc.args, out)
				}
			}
			// The changeset arms belong to the changeset modes. A path
			// operand names its own file set; there is nothing to assume.
			for _, unwanted := range []string{"assuming runtime", scopeEmptyPrefix} {
				if strings.Contains(errOut, unwanted) {
					t.Errorf("a named path is a computed classification, not an assumed one; stderr said %q", errOut)
				}
			}
		})
	}
}

// An absolute path under the root is the same question spelled differently —
// the form a CI router built from `git diff --name-only` prefixed with
// $GITHUB_WORKSPACE produces.
func TestScopeAcceptsAnAbsoluteInRootPathOperand(t *testing.T) {
	root := scopeOperandRepo(t)
	code, out, errOut := runCLI(t, "scope", "-C", root, filepath.Join(root, "docs", "a.md"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "class=docs") {
		t.Fatalf("an absolute in-root path must be relativized and classified:\n%s", out)
	}
}

// The Introspection preface's own promise: "an out-of-root path is exit 2 — an
// empty answer to a wrong question would be a guidance fail-open." rules-for
// has always kept it; scope answered `class=runtime`, exit 0.
func TestScopeRefusesAnOutOfRootPathOperand(t *testing.T) {
	root := scopeOperandRepo(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	for _, tc := range []struct {
		name string
		arg  string
	}{
		{"absolute", outside},
		{"relative", filepath.Join("..", "outside.md")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCLI(t, "scope", "-C", root, tc.arg)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 — a confident class for a path outside the root is a guidance fail-open\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
			}
			if strings.Contains(out, "class=") {
				t.Errorf("a refused query must print no classification:\n%s", out)
			}
			if !strings.Contains(errOut, "outside the repository root") {
				t.Errorf("stderr must name the cause, got %q", errOut)
			}
		})
	}
}

// A directory cannot be classified: its files may fall in different classes,
// and the bare directory string matches file globs by accident or not at all.
// Same refusal rules-for gives, and it is a distinct branch of the shared
// normalizer, so it gets its own case.
func TestScopeRefusesADirectoryOperand(t *testing.T) {
	root := scopeOperandRepo(t)
	code, out, errOut := runCLI(t, "scope", "-C", root, "docs")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "class=") {
		t.Errorf("a refused query must print no classification:\n%s", out)
	}
	if !strings.Contains(errOut, "directory") {
		t.Errorf("stderr must name the cause, got %q", errOut)
	}
}

// THE AMPLIFICATION. flag.Parse stops at the first non-flag argument, so a mode
// flag written after a path operand never reaches the flag set: it was silently
// discarded and the run became a staged-set run. Refusing is the only
// fail-closed answer — honouring it would require re-ordering argv behind the
// operator's back, and ignoring it is how a bogus --range became a confident
// exit-0 class.
func TestScopeRefusesAFlagAfterAPathOperand(t *testing.T) {
	root := scopeOperandRepo(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"range", []string{"docs/a.md", "--range", "bogusref..HEAD"}},
		{"staged", []string{"docs/a.md", "--staged"}},
		{"format", []string{"docs/a.md", "-format", "json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCLI(t, append([]string{"scope", "-C", root}, tc.args...)...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 — a flag the parser never saw must not become a silent different run\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
			}
			if strings.Contains(out, "class=") {
				t.Errorf("a refused invocation must print no classification:\n%s", out)
			}
			if !strings.Contains(errOut, tc.args[1]) {
				t.Errorf("stderr must name the discarded flag %q, got %q", tc.args[1], errOut)
			}
		})
	}
}

// Path operands and a changeset selector are two different questions. Answering
// either one silently would be a wrong answer to the other.
func TestScopeRefusesPathOperandsCombinedWithAChangesetSelector(t *testing.T) {
	root := scopeOperandRepo(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"staged", []string{"--staged", "docs/a.md"}},
		{"range", []string{"--range", "HEAD..HEAD", "docs/a.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCLI(t, append([]string{"scope", "-C", root}, tc.args...)...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
			}
			if strings.Contains(out, "class=") {
				t.Errorf("a refused invocation must print no classification:\n%s", out)
			}
			if !strings.Contains(errOut, tc.args[0]) {
				t.Errorf("stderr must name the selector %q it cannot combine with a path, got %q", tc.args[0], errOut)
			}
		})
	}
}

// Backwards compatibility, pinned rather than assumed: bare `formwork scope`
// still classifies the changeset. AGENTS.md and public-AGENTS.md both document
// that form, and adding an operand must not have moved it.
func TestScopeWithNoOperandsStillClassifiesTheChangeset(t *testing.T) {
	root := scopeOperandRepo(t)
	code, out, errOut := runCLI(t, "scope", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "class=runtime") || !strings.Contains(out, "go_changed=true") {
		t.Fatalf("the staged .go file must still drive a bare `scope`:\n%s", out)
	}
}
