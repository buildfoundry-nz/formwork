// no_shell_test.go — this repository tracks NO shell scripts.
//
// The product's claim is that a fleet of shell gate scripts should be one
// binary with tests. A repository selling that while gating itself with bash is
// arguing against itself in the first place a reader looks — and the public
// tree is exactly where a reader looks first.
//
// THIS IS A TEST, NOT A FORMWORK RULE, deliberately. A rule needs a fire
// fixture, and a fire fixture for "no .sh may be tracked" is itself a tracked
// .sh — the lockdown would have to violate the thing it locks down. Asking git
// directly needs no fixture at all.
//
// There is no keep list for TOOLING. Not "fewer", not "the product gates
// only", not "the dev-convenience ones" — a launcher can be a Go program. The
// one exception described below is not tooling at all, and it is twenty named
// paths rather than a shape precisely so it cannot quietly become a keep
// list.
//
// WHAT COUNTS AS A SCRIPT is the file, not its name (#303). The first cut of
// this lockdown asked git for `*.sh` and `*.py`, which is an extension match
// and a case-sensitive one, so `tools/dev/check-thing` with a bash shebang and
// the executable bit — the ordinary spelling for an executable in bin/ or
// tools/ — passed, and so did GATE.SH and tool.bash. Every tracked file's
// first line is read here and its interpreter classified.
//
// THE ONE SCOPE BOUNDARY, stated rather than buried: a fixture tree under
// examples/<corpus>/.formwork/fixtures/ is INPUT BYTES a rule is tested
// against, not tooling this repository runs. Twenty `#!/usr/bin/env sh` husky
// hooks live there because the corpus rules being ported are ABOUT husky
// hooks — gate-scripts-are-wired and git-hooks-fully-wired cannot have a fire
// fixture that is not a hook, and a husky hook is invoked by name so it cannot
// be renamed to .go. It is an ENUMERATED list of twenty paths (huskyShims),
// not a path shape (#264 r2): a twenty-first hook is reported like any other
// tracked shell script, and a pin the index stops carrying is reported too, so
// the exception can neither grow nor rot in silence. This repository's OWN
// .formwork/fixtures/ is inside the ban, and so is every other path under
// examples/. What the boundary does not buy is executability — twelve of the
// twenty carry mode 0755 — so it is a statement about what the file is FOR,
// backed by the fact that nothing in this tree invokes one.
package repoproof_test

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// gitScrubbed builds a git command that cannot be steered by the environment.
//
// GIT_DIR, GIT_INDEX_FILE and GIT_WORK_TREE each point git at a different
// repository, index or tree. A lockdown that inherits them answers a question
// about somewhere else and reports it as a pass here (#264 r1) — the pointer
// family #175 burned this repo on, which internal/vcs already scrubs for
// exactly this reason.
func gitScrubbed(dir string, args ...string) *exec.Cmd {
	// REMOVED, not blanked: git reads an empty GIT_DIR as a directory named ""
	// and dies with "not a git repository", which would turn a fail-open into a
	// permanent fail — right direction, wrong answer.
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_INDEX_FILE": true, "GIT_WORK_TREE": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_COMMON_DIR": true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_CEILING_DIRECTORIES": true,
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && drop[name] {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	return cmd
}

// trackedFiles lists every path git has in the index for dir.
func trackedFiles(t *testing.T, dir string, pathspec ...string) []string {
	t.Helper()
	needBinary(t, "git")
	out, err := gitScrubbed(dir, append([]string{"ls-files", "-z", "--"}, pathspec...)...).Output()
	if err != nil {
		t.Fatalf("cannot ask git for the tracked file list: %v", err)
	}
	var found []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			found = append(found, p)
		}
	}
	return found
}

// scriptExt maps the extensions that settle the question on their own,
// lower-cased by the caller so GATE.SH is a shell script.
var scriptExt = map[string]string{
	".sh": "shell", ".bash": "shell", ".zsh": "shell", ".ksh": "shell", ".dash": "shell",
	".py": "python", ".pyw": "python",
}

// interpreter picks the program out of a shebang line: the basename of the
// first word, or — for `env` — the first word after it that is neither an
// option nor a VAR=value assignment, which is how `#!/usr/bin/env -S bash -eu`
// spells itself.
var shellInterp = regexp.MustCompile(`^(sh|bash|dash|ksh|zsh|ash)$`)

func interpreterKind(head []byte) string {
	line, _, _ := bytes.Cut(head, []byte("\n"))
	text := strings.TrimRight(string(line), "\r")
	if !strings.HasPrefix(text, "#!") {
		return ""
	}
	fields := strings.Fields(strings.TrimPrefix(text, "#!"))
	if len(fields) == 0 {
		return ""
	}
	name := filepath.Base(fields[0])
	if name == "env" {
		name = ""
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") || strings.Contains(f, "=") {
				continue
			}
			name = filepath.Base(f)
			break
		}
	}
	switch {
	case shellInterp.MatchString(name):
		return "shell"
	case strings.HasPrefix(name, "python"):
		return "python"
	}
	return ""
}

// scriptKind classifies a tracked file as "shell", "python" or "" (neither),
// from its extension OR its shebang.
func scriptKind(path string, head []byte) string {
	if k, ok := scriptExt[strings.ToLower(filepath.Ext(path))]; ok {
		return k
	}
	return interpreterKind(head)
}

// census is what one walk of the index found: the scripts the lockdown
// reports, split by interpreter, the scripts the corpus-fixture exception
// covers, the pinned exceptions the index no longer carries, and the tracked
// paths it could not read.
type census struct {
	byKind     map[string][]string
	exempt     []string
	stale      []string
	unreadable []string
}

// takeCensus classifies each path. head returns the first bytes of one path;
// a read failure is RECORDED, never skipped, because a lockdown that silently
// drops a file from its own subject list is the defect it exists to prevent.
//
// The corpus-fixture exception is applied AFTER classification and by name, so
// what it excuses is counted (exempt) and what it names but no longer covers is
// reported (stale).
func takeCensus(paths []string, head func(path string) ([]byte, error)) census {
	c := census{byKind: map[string][]string{}}
	tracked := map[string]bool{}
	for _, p := range paths {
		slash := filepath.ToSlash(p)
		tracked[slash] = true
		b, err := head(p)
		if err != nil {
			c.unreadable = append(c.unreadable, p+": "+err.Error())
			continue
		}
		k := scriptKind(slash, b)
		if k == "" {
			continue
		}
		if huskyShims[slash] {
			c.exempt = append(c.exempt, slash)
			continue
		}
		c.byKind[k] = append(c.byKind[k], slash)
	}
	for p := range huskyShims {
		if !tracked[p] {
			c.stale = append(c.stale, p)
		}
	}
	slices.Sort(c.stale)
	return c
}

// repoCensus walks THIS repository's index and reads each file's first line.
func repoCensus(t *testing.T, root string) census {
	t.Helper()
	return takeCensus(trackedFiles(t, root), func(p string) ([]byte, error) {
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		head := make([]byte, 256)
		n, _ := f.Read(head)
		return head[:n], nil
	})
}

func TestNoTrackedShellScripts(t *testing.T) {
	c := repoCensus(t, repoRoot(t))
	if len(c.unreadable) > 0 {
		t.Fatalf("%d tracked file(s) could not be read, so they were never classified — a "+
			"lockdown that drops files from its own subject list proves nothing:\n  %s",
			len(c.unreadable), strings.Join(c.unreadable, "\n  "))
	}
	if found := c.byKind["shell"]; len(found) > 0 {
		t.Fatalf("%d tracked shell script(s) — the target is ZERO, and there is no keep bucket. "+
			"A launcher can be a Go program:\n  %s", len(found), strings.Join(found, "\n  "))
	}
}

func TestNoTrackedPythonScripts(t *testing.T) {
	c := repoCensus(t, repoRoot(t))
	if len(c.unreadable) > 0 {
		t.Fatalf("%d tracked file(s) could not be read, so they were never classified:\n  %s",
			len(c.unreadable), strings.Join(c.unreadable, "\n  "))
	}
	if found := c.byKind["python"]; len(found) > 0 {
		t.Fatalf("%d tracked python script(s) — this is a Go repository:\n  %s",
			len(found), strings.Join(found, "\n  "))
	}
}

// #303 / #264 r2 — the predicate must read the file, not its name.
//
// `git ls-files '*.sh'` is an extension match and a case-sensitive one. An
// executable bash gate spelled `tools/dev/check-thing` — the ordinary spelling
// for an executable in bin/ or tools/ — rides straight through, and so do
// GATE.SH and tool.bash. The repository could therefore reacquire an unlimited
// number of tracked shell gates while the lockdown that forbids them stays
// green, which is not a hypothetical: the review added exactly that file to the
// index and both tests passed.
func TestScriptKindReadsTheFileNotTheName(t *testing.T) {
	for _, v := range []struct {
		label string
		path  string
		head  string
		want  string
	}{
		{"a plain .sh", "scripts/gate.sh", "echo hi\n", "shell"},
		{"a shouty .SH", "scripts/GATE.SH", "echo hi\n", "shell"},
		{"a .bash", "tools/lib.bash", "echo hi\n", "shell"},
		{"a .zsh", "tools/lib.zsh", "echo hi\n", "shell"},
		{"a .ksh", "tools/lib.ksh", "echo hi\n", "shell"},
		{"a .py", "tools/gen.py", "print(1)\n", "python"},
		{"a .PY", "tools/GEN.PY", "print(1)\n", "python"},
		{"an extensionless bash gate", "tools/dev/check-thing",
			"#!/usr/bin/env bash\nset -euo pipefail\ngrep -q foo bar || exit 1\n", "shell"},
		{"an extensionless sh gate", "bin/preflight", "#!/bin/sh\nexit 0\n", "shell"},
		{"an absolute-path bash shebang", "bin/x", "#!/bin/bash\n", "shell"},
		{"a zsh shebang", "bin/z", "#!/usr/bin/env zsh\n", "shell"},
		{"an env -S bash shebang", "bin/s", "#!/usr/bin/env -S bash -eu\n", "shell"},
		{"an extensionless python gate", "tools/dev/gen", "#!/usr/bin/env python3\nprint(1)\n", "python"},
		{"an absolute-path python shebang", "bin/p", "#!/usr/bin/python3.11\n", "python"},
		{"a Go file", "internal/engine/engine.go", "package engine\n", ""},
		{"a Go file whose first line mentions a shebang", "internal/hooks/shim.go",
			"package hooks // writes a #!/bin/sh shim\n", ""},
		{"a shell command line inside a YAML string", ".github/workflows/ci.yml",
			"name: ci\njobs:\n  x:\n    steps:\n      - run: bash -c true\n", ""},
		{"a leading blank line before a shebang", "bin/late", "\n#!/bin/bash\n", ""},
		{"an empty file", "docs/EMPTY", "", ""},
	} {
		if got := scriptKind(v.path, []byte(v.head)); got != v.want {
			t.Errorf("%s (%s): scriptKind = %q, want %q", v.label, v.path, got, v.want)
		}
	}
}

// #264 r1 — the lookup must not be steerable by the environment.
//
// GIT_INDEX_FILE, GIT_DIR and GIT_WORK_TREE each point git somewhere else, and
// the lockdown inherited the whole environment: with a committed evil.sh in the
// real index, `GIT_INDEX_FILE=<an empty index> go test` reported a pass. This
// is the pointer family #175 burned this repo on; internal/vcs scrubs it, and
// so must anything that asks git a question whose answer is a gate verdict.
func TestTrackedFilesIgnoresAHostileGitEnvironment(t *testing.T) {
	needBinary(t, "git")
	root := repoRoot(t)
	elsewhere := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet", "-b", "main", "."}} {
		c := exec.Command("git", args...)
		c.Dir = elsewhere
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("building the decoy repo: %v: %s", err, out)
		}
	}
	honest := trackedFiles(t, root, "go.mod")
	if len(honest) != 1 {
		t.Fatalf("the control lookup did not find go.mod (%v) — the vectors below would prove nothing", honest)
	}
	for _, v := range []struct{ name, value string }{
		{"GIT_INDEX_FILE", filepath.Join(elsewhere, ".git", "index")},
		{"GIT_DIR", filepath.Join(elsewhere, ".git")},
		{"GIT_WORK_TREE", elsewhere},
	} {
		t.Run(v.name, func(t *testing.T) {
			t.Setenv(v.name, v.value)
			if got := trackedFiles(t, root, "go.mod"); len(got) != 1 {
				t.Errorf("%s=%s steered the lookup away from the repository under test "+
					"(got %v) — the lockdown then reports a pass over an index it never read",
					v.name, v.value, got)
			}
		})
	}
}

// huskyShims enumerates the corpus-fixture exception, path by path.
//
// These twenty files are INPUT BYTES a rule is tested against, not tooling
// this repository runs: examples/palletra-port-full's gate-scripts-are-wired
// and git-hooks-fully-wired rules are ABOUT husky hooks, and a fire fixture
// for "the hooks are wired" cannot be anything but a hook. A husky hook is
// invoked by name — .husky/pre-commit — so it cannot be renamed to .go and it
// cannot be renamed to anything else either.
//
// Enumerated rather than shaped so it cannot grow in silence. Every entry is
// under ONE corpus's .formwork/fixtures/ tree; this repository's own
// .formwork/fixtures/ is not excused, and neither is any other path under
// examples/. Nothing in this tree invokes one of these.
var huskyShims = map[string]bool{
	"examples/palletra-port-full/.formwork/fixtures/gate-scripts-are-wired/fire-1/.husky/pre-commit":               true,
	"examples/palletra-port-full/.formwork/fixtures/gate-scripts-are-wired/pass-1/.husky/pre-commit":               true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/fire-1/.husky/pre-commit":                true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/fire-1/.husky/pre-push":                  true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/pass-1/.husky/pre-commit":                true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/pass-1/.husky/pre-push":                  true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/fire-2/.husky/pre-push":                  true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/fire-3/.husky/pre-commit":                true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/fire-3/.husky/pre-push":                  true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/fire-4/.husky/pre-push":                  true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/pass-2/.husky/pre-commit":                true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/pass-2/.husky/pre-push":                  true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/pass-3/.husky/pre-commit":                true,
	"examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/pass-3/.husky/pre-push":                  true,
	"examples/palletra-port-full/.formwork/fixtures/schema-regen-none-branch-cardinality/fire-1/.husky/pre-commit": true,
	"examples/palletra-port-full/.formwork/fixtures/schema-regen-none-branch-cardinality/pass-1/.husky/pre-commit": true,
	"examples/palletra-port-full/.formwork/fixtures/schema-regen-single-assignment/fire-1/.husky/pre-commit":       true,
	"examples/palletra-port-full/.formwork/fixtures/schema-regen-single-assignment/pass-1/.husky/pre-commit":       true,
	"examples/palletra-port-full/.formwork/fixtures/schema-regen-skip-branch-cardinality/fire-1/.husky/pre-commit": true,
	"examples/palletra-port-full/.formwork/fixtures/schema-regen-skip-branch-cardinality/pass-1/.husky/pre-commit": true,
}

// #264 r2 — the corpus-fixture exception must be an enumerated pin, not a
// path shape.
//
// `^examples/[^/]+/\.formwork/fixtures/` covers twenty husky hooks today, and
// it would cover a twenty-first, and a hundredth, without anything saying so:
// the shape is tested BEFORE the file is read, so a script inside it is
// dropped from the subject list before it is ever classified. An exception
// that cannot count what it is excusing is the same defect as a predicate
// that cannot read the file it is judging — the reader is told twenty corpus
// hooks are excused and has no way to find out that is still true.
//
// The pin has to be exact in BOTH directions. A pinned path the index no
// longer carries is a dead entry that quietly re-opens the hole it names, so
// it is reported too.
func TestCorpusFixtureExceptionIsPinnedNotShaped(t *testing.T) {
	const (
		corpus     = "examples/palletra-port-full/.formwork/fixtures/git-hooks-fully-wired/"
		pinned     = corpus + "fire-1/.husky/pre-commit"
		alsoPinned = corpus + "pass-1/.husky/pre-push"
		unpinned   = corpus + "fire-2/.husky/pre-commit"
		notAScript = corpus + "fire-1/.husky/_/husky.sh.md"
	)
	head := func(p string) ([]byte, error) {
		if strings.HasSuffix(p, ".md") {
			return []byte("# what this fixture depicts\n"), nil
		}
		return []byte("#!/usr/bin/env sh\nnpx lint-staged\n"), nil
	}

	t.Run("a pinned husky shim is excused and counted", func(t *testing.T) {
		c := takeCensus([]string{pinned, alsoPinned}, head)
		if !slices.Contains(c.exempt, pinned) {
			t.Errorf("the exception dropped %s without counting it (exempt=%v) — nothing "+
				"can tell the reader how many hooks it is excusing", pinned, c.exempt)
		}
		if got := c.byKind["shell"]; len(got) > 0 {
			t.Errorf("a pinned husky shim was reported as a tracked shell script: %v", got)
		}
	})

	t.Run("an unpinned hook of the same shape is reported", func(t *testing.T) {
		c := takeCensus([]string{pinned, unpinned}, head)
		if !slices.Contains(c.byKind["shell"], unpinned) {
			t.Errorf("%s rode through on the path SHAPE (shell=%v, exempt=%v) — the corpus "+
				"fixture tree can grow an unlimited number of tracked shell scripts while "+
				"the lockdown stays green", unpinned, c.byKind["shell"], c.exempt)
		}
		if slices.Contains(c.exempt, unpinned) {
			t.Errorf("%s was excused without ever being pinned", unpinned)
		}
	})

	t.Run("a pin the index no longer carries is reported", func(t *testing.T) {
		c := takeCensus([]string{pinned}, head)
		if !slices.Contains(c.stale, alsoPinned) {
			t.Errorf("%s is pinned as an excused husky shim but was not in the index, and "+
				"the census said nothing (stale=%v) — a dead pin re-opens the hole it names",
				alsoPinned, c.stale)
		}
	})

	t.Run("a non-script inside the boundary is neither excused nor reported", func(t *testing.T) {
		c := takeCensus([]string{notAScript}, head)
		if slices.Contains(c.exempt, notAScript) || slices.Contains(c.byKind["shell"], notAScript) {
			t.Errorf("%s is not a script and belongs in neither list (exempt=%v, shell=%v)",
				notAScript, c.exempt, c.byKind["shell"])
		}
	})
}

// The pin over THIS repository: every path it names is still tracked, and the
// census of excused scripts is exactly the pinned set. A shim that is deleted
// or renamed leaves an entry behind that excuses a path nothing occupies —
// until someone puts a script back at that exact path.
func TestHuskyShimExceptionIsExact(t *testing.T) {
	c := repoCensus(t, repoRoot(t))
	if len(c.stale) > 0 {
		t.Errorf("%d pinned husky shim(s) are no longer tracked — remove them from the pin, "+
			"because an entry that excuses a path nothing occupies is an open door with a "+
			"label on it:\n  %s", len(c.stale), strings.Join(c.stale, "\n  "))
	}
	want := slices.Sorted(maps.Keys(huskyShims))
	got := slices.Clone(c.exempt)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("the excused census is not the pinned set.\n  excused: %v\n  pinned:  %v", got, want)
	}
}
