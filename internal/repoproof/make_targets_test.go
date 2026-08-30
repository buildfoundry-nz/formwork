// make_targets_test.go — the Makefile's own gates cannot report a pass over a
// subject they never read.
//
// Three shapes of the same failure, all of them live at 887acefa:
//
//	fmt      exits 0 whenever `git ls-files` cannot answer (#276). The OSS cut
//	         tree has no .git by construction and is the tree the Makefile's
//	         own publication-cut comment calls the readiness signal, so the
//	         one gate that reads formatting is silent in exactly the tree it
//	         is quoted for. A release tarball and a PATH without git are the
//	         same hole.
//
//	-run     `go test <pkg> -run <re>` exits 0 with "[no tests to run]" when
//	         the regex matches nothing (#278), so deleting the tests a proof
//	         target names leaves the target printing a green `ok` line over
//	         the broken property. Ten targets have that shape.
//
//	.PHONY   a target make does not know is phony is a FILE target: the moment
//	         a file of that name exists in the repo root, make reports "up to
//	         date", the recipe never runs, and the exit status is 0 (#317) —
//	         inside `make verify`, which then also exits 0.
//
// The through-line: an absent or unreadable SUBJECT reads as a pass. Every
// assertion below fixes the reading, never the symptom.
package repoproof_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readMakefile(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("cannot read the Makefile this gate judges: %v", err)
	}
	return string(data)
}

// shimPATH builds a directory holding exactly the executables named, so a
// recipe can be driven against a deliberately incomplete environment. A real
// tool is symlinked; a tool named with a body is written as that body.
func shimPATH(t *testing.T, real []string, fake map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range real {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("%s is not on PATH, so this gate cannot answer", name)
		}
		if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range fake {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runMake runs one make invocation from the repo root and returns its exit
// status and merged output. A non-empty pathDir REPLACES PATH, which is how a
// recipe is driven against a deliberately incomplete environment; args are the
// target and any make-style assignments after it.
func runMake(t *testing.T, pathDir string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command("make", args...)
	cmd.Dir = repoRoot(t)
	if pathDir != "" {
		cmd.Env = append(os.Environ(), "PATH="+pathDir)
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("make %s could not run: %v", strings.Join(args, " "), err)
	}
	return code, string(out)
}

// #276. `fmt` keys off `git ls-files` so that gitignored clones under
// projects/ stay out. That lookup has FOUR failure modes and every one of them
// read as "nothing to format" before the fix: git absent, git answering
// non-zero (the OSS-cut tree, which has no .git by construction), git
// answering EMPTY, and gofmt itself absent — in that last case the assignment
// captures no stdout and the emptiness test passes. A formatting gate that
// cannot see the file set must say so.
//
// Each vector asserts the REASON the refusal names, not merely a non-zero
// status. A status-only assertion is satisfied by any make failure at all:
// measured, replacing the entire recipe with `@echo "fmt: refusing" >&2; exit 2`
// — a target that never reads a file and never runs gofmt — left the
// status-only version of this test green over all three of its vectors.
func TestFmtRefusesWhenItCannotReadTheFileSet(t *testing.T) {
	needBinary(t, "make")
	gitFails := "#!/bin/sh\necho 'fatal: not a git repository (or any of the parent directories): .git' >&2\nexit 128\n"
	gitEmpty := "#!/bin/sh\nexit 0\n"
	for _, v := range []struct {
		label string
		real  []string
		fake  map[string]string
		names string
	}{
		{"git answers non-zero", []string{"gofmt"}, map[string]string{"git": gitFails}, "git ls-files failed"},
		{"git is not on PATH", []string{"gofmt"}, nil, "git ls-files failed"},
		{"git ls-files answers empty", []string{"gofmt"}, map[string]string{"git": gitEmpty}, "matched nothing"},
		{"gofmt is not on PATH", []string{"git"}, nil, "gofmt could not judge"},
	} {
		code, out := runMake(t, shimPATH(t, v.real, v.fake), "fmt")
		if code == 0 {
			t.Errorf("fmt with %s exited 0 — a formatting gate that never read a file "+
				"must not report a formatting pass:\n%s", v.label, out)
			continue
		}
		if !strings.Contains(out, v.names) {
			t.Errorf("fmt with %s exited %d but never named %q as the reason — a status-only "+
				"assertion is satisfied by a deleted target, a syntax error, or a recipe that "+
				"refuses everything, none of which is this gate working:\n%s",
				v.label, code, v.names, out)
		}
	}
}

// #276, the direction the refusal arm above cannot supply. Three refusals are
// a NEGATIVE-only proof, and this Makefile's own gate-proof comment says what
// that is worth: "a negative-only proof passes just as well against a gate
// that always fails". So `fmt` is also shown to READ the set git names and to
// JUDGE it — the unformatted file git hands it comes back by name under
// "gofmt needed:", and the real tree under a real PATH comes back clean.
//
// Both halves are needed. Without the first, a recipe that refuses everything
// passes the refusal arm; without the second, one that ignores git's answer
// and always reports "gofmt needed" passes the first.
func TestFmtJudgesTheSetGitNamesAndPassesTheRealTree(t *testing.T) {
	needBinary(t, "make")
	needBinary(t, "gofmt")

	bad := filepath.Join(t.TempDir(), "unformatted.go")
	if err := os.WriteFile(bad, []byte("package p\n\nfunc  Bad( ) {\n\tx :=1\n_ = x\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// `echo` is a shell builtin, so the shim needs nothing on the PATH it is
	// handed beyond gofmt — which is the point of the shim.
	gitNames := "#!/bin/sh\necho '" + bad + "'\n"
	code, out := runMake(t, shimPATH(t, []string{"gofmt"}, map[string]string{"git": gitNames}), "fmt")
	if code == 0 {
		t.Errorf("fmt reported a pass over a file gofmt calls unformatted:\n%s", out)
	}
	if !strings.Contains(out, "gofmt needed:") || !strings.Contains(out, bad) {
		t.Errorf("fmt must NAME the unformatted file git handed it, or it is not judging that "+
			"set at all; wanted %q under a `gofmt needed:` line:\n%s", bad, out)
	}

	// The real tree, the real PATH. This repo is gofmt-clean and `make verify`
	// depends on it, so a red here means either the tree regressed or `fmt` has
	// become a target that refuses whatever it is given.
	if code, out := runMake(t, "", "fmt"); code != 0 {
		t.Errorf("fmt over the real tree exited %d — every refusal arm above is "+
			"indistinguishable from a gate that always fails unless this passes:\n%s", code, out)
	}
}

// proofCall matches a `$(call proof,<pkg>,<regex>)` recipe line: the one
// spelling a -run-filtered target is allowed to have.
var proofCall = regexp.MustCompile(`\$\(call proof,([^,)]+),([^)]*)\)`)

// recipes maps each target to its own recipe text, skipping the body of any
// `define` — a define is the mechanism, not a target, and the `proof` define
// legitimately spells the thing every target is forbidden to spell.
func recipes(mk string) map[string]string {
	def := regexp.MustCompile(`^([a-z][a-z0-9-]*):`)
	out := map[string]string{}
	cur := ""
	inDefine := false
	for _, l := range strings.Split(mk, "\n") {
		switch {
		case strings.HasPrefix(l, "define "):
			inDefine = true
			continue
		case strings.HasPrefix(l, "endef"):
			inDefine = false
			continue
		}
		if inDefine {
			continue
		}
		if m := def.FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		if strings.HasPrefix(l, "\t") && cur != "" {
			out[cur] += l + "\n"
		}
	}
	return out
}

// #278, direction one — SHAPE. A recipe that runs a -run-filtered `go test`
// must ENUMERATE the selection first, because `go test -run` exits 0 over an
// empty one. `go test -list` is the enumeration, and `$(call proof,...)` is
// the packaged form of it. Stated as "count before you run" rather than as a
// list of blessed targets, so a new target acquires the obligation by being
// written rather than by someone remembering to add it here.
func TestNoRecipeRunsARunFilteredGoTestWithoutCountingTheSelection(t *testing.T) {
	found := 0
	for name, recipe := range recipes(readMakefile(t)) {
		if !strings.Contains(recipe, "go test") || !strings.Contains(recipe, "-run") {
			continue
		}
		found++
		if strings.Contains(recipe, "$(call proof,") || strings.Contains(recipe, "-list") {
			continue
		}
		t.Errorf("target %q runs a -run-filtered `go test` without counting the selection:\n%s"+
			"`go test <pkg> -run <re>` exits 0 with \"[no tests to run]\" when the regex "+
			"matches nothing, so the target reports a pass over tests that are gone. "+
			"Use $(call proof,<pkg>,<regex>), or count with `go test -list` and refuse zero.",
			name, recipe)
	}
	if found == 0 {
		t.Fatal("no -run-filtered recipe found at all — this test is parsing the wrong Makefile " +
			"and would report a pass over nothing")
	}
}

// goTestAtCommandPosition matches an invocation, never a mention: `go test`
// has to sit where a command can start. Without the anchor a recipe that
// merely PRINTS advice about `go test` reads as one — two of this Makefile's
// own refusal messages do exactly that.
var goTestAtCommandPosition = regexp.MustCompile(`(^|[\s;|&({@])go test\b`)

// blankDoubleQuoted replaces the CONTENTS of every double-quoted span with
// spaces, leaving offsets intact. Same reason the rule engine destrings before
// matching: a gate that reads inside a string literal fires on the text
// describing it rather than on the code doing it.
func blankDoubleQuoted(s string) string {
	b := []byte(s)
	in := false
	for i := range b {
		if b[i] == '"' {
			in = !in
			continue
		}
		if in {
			b[i] = ' '
		}
	}
	return string(b)
}

// The build cache. Every other executing `go test` in this Makefile already
// spells -count=1: the `proof` define does, and hooks-proof does. `make test`
// — the one target that runs the WHOLE suite, and the first prerequisite of
// `make verify` — is the only one that does not, so it reports whatever the
// cache last recorded rather than a run it forced.
//
// That is not theoretical, and it is the same "pass over a subject nothing
// read" shape as the three at the top of this file. A test that BUILDS a
// sibling command and execs it has a cache key covering its own package only;
// change the command, and the stale `ok` outlives the change.
// internal/denylist/e2e_test.go is exactly that shape, and `make verify`
// exited 0 over a suite that fails under -count=1 because of it.
//
// Stated over every executing invocation rather than over `test` by name, so a
// new recipe acquires the obligation by being written rather than by someone
// remembering to add it here. `-list` enumerates without running and is
// exempt.
func TestNoGoTestInvocationTrustsTheBuildCache(t *testing.T) {
	found := 0
	for _, line := range strings.Split(readMakefile(t), "\n") {
		code := blankDoubleQuoted(line)
		if i := strings.Index(code, "#"); i >= 0 {
			code = code[:i]
		}
		if !goTestAtCommandPosition.MatchString(code) || strings.Contains(code, "-list") {
			continue
		}
		found++
		if strings.Contains(code, "-count=1") || strings.Contains(code, "-count 1") {
			continue
		}
		t.Errorf("this `go test` runs the suite but never forces it to execute:\n\t%s\n"+
			"go caches a passing test binary's result, and a test that builds and execs a "+
			"sibling command has a cache key that does not cover that command — so a stale "+
			"`ok` outlives the change and the target reports a pass over a run it did not "+
			"make. Spell -count=1, the way $(call proof,...) and hooks-proof already do.",
			strings.TrimSpace(line))
	}
	if found == 0 {
		t.Fatal("no executing `go test` invocation found at all — this test is parsing the " +
			"wrong Makefile and would report a pass over nothing")
	}
}

// #278, direction two — LIVE. Every proof call must select at least one test
// right now. The shape arm above cannot see a regex that has drifted off the
// tests it names; this one runs the selection and counts it.
func TestEveryProofCallSelectsAtLeastOneTest(t *testing.T) {
	needBinary(t, "go")
	root := repoRoot(t)
	calls := proofCall.FindAllStringSubmatch(readMakefile(t), -1)
	if len(calls) == 0 {
		t.Fatal("no $(call proof,...) recipes found — either the mechanism is gone or this " +
			"test is parsing the wrong Makefile; refusing to report a pass over zero targets")
	}
	for _, m := range calls {
		pkg, re := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		cmd := exec.Command("go", "test", pkg, "-list", re)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("go test %s -list %q failed, so the selection cannot be counted: %v\n%s", pkg, re, err, out)
			continue
		}
		n := 0
		for _, l := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(l, "Test") {
				n++
			}
		}
		if n == 0 {
			t.Errorf("$(call proof,%s,%s) selects NO test — the target bears the name of a "+
				"property nothing checks", pkg, re)
		}
	}
}

// #278, the crudest direction of all, and the one the two arms above cannot
// see. Both of them start from a `$(call proof,...)` that is still written: the
// shape arm skips a recipe with no `go test` in it, and the live arm counts the
// calls it finds. A target that stops calling anything is invisible to both —
// measured, replacing `gate-proof`'s recipe with `@true; exit 0` left the whole
// internal/repoproof package green and `make gate-proof` exiting 0.
//
// So a target that calls itself a proof must RUN something. The four spellings
// are the only ways this Makefile executes code, and they are listed here
// rather than blessed per-target, so a target acquires the obligation by being
// named `-proof` rather than by someone remembering to add it.
//
// WHAT IT DOES NOT COVER: `corpus-denylist` and `corpus-structural` are verify
// prerequisites that run formwork's own binaries without carrying the suffix,
// so a wholesale disarm of either is still invisible here.
func TestEveryProofTargetRunsSomething(t *testing.T) {
	runsCode := []string{"$(call proof,", "$(call GO_RUN,", "go test", "go run"}
	found := 0
	for name, recipe := range recipes(readMakefile(t)) {
		if !strings.HasSuffix(name, "-proof") {
			continue
		}
		found++
		ran := false
		for _, spelling := range runsCode {
			if strings.Contains(recipe, spelling) {
				ran = true
				break
			}
		}
		if !ran {
			t.Errorf("target %q is named a proof but runs none of %v:\n%s"+
				"A recipe that executes nothing exits 0, and inside `make verify` so does the "+
				"gate. If it runs code by a spelling not listed here, add that spelling beside "+
				"the reason rather than exempting the target.", name, runsCode, recipe)
		}
	}
	if found == 0 {
		t.Fatal("no -proof target found at all — this test is parsing the wrong Makefile and " +
			"would report a pass over nothing")
	}
}

// #278 one level up, which is #276's shape. `define proof` closes the
// empty-selection hole by ENUMERATING with `go test -list` before it runs
// anything — and that makes the enumeration a new lookup which can itself
// fail. A lookup that cannot answer must not read as a pass, so both ways it
// can fail are driven here against a real proof target.
//
// The shape arm above reads the Makefile as text and the live arm below
// re-implements the count in Go; neither executes the define. This does, which
// is the only way the define's own refusals are covered at all.
func TestProofDefineRefusesASubjectItCannotEnumerate(t *testing.T) {
	needBinary(t, "make")
	for _, v := range []struct{ label, body, names string }{
		{
			"go cannot run at all",
			"#!/bin/sh\necho 'go: cannot execute binary file' >&2\nexit 126\n",
			"cannot list tests matching",
		},
		{
			"go answers, and lists nothing",
			"#!/bin/sh\nexit 0\n",
			"matches NO test",
		},
	} {
		// grep is the only external the define needs; printf, echo and [ are
		// shell builtins, so the shim PATH is deliberately this small.
		code, out := runMake(t, shimPATH(t, []string{"grep"}, map[string]string{"go": v.body}), "sync-proof")
		if code == 0 {
			t.Errorf("sync-proof with %s exited 0 — a proof that could not enumerate its own "+
				"subject reported a pass over it:\n%s", v.label, out)
			continue
		}
		if !strings.Contains(out, v.names) {
			t.Errorf("sync-proof with %s exited %d but never named %q — a status-only assertion "+
				"here is satisfied by make failing for any reason:\n%s", v.label, code, v.names, out)
		}
	}

	// The direction the two refusals cannot supply: with a real toolchain the
	// same target enumerates, runs and passes. Without this, a `proof` define
	// that refused every subject would satisfy the arms above.
	needBinary(t, "go")
	if code, out := runMake(t, "", "sync-proof"); code != 0 {
		t.Errorf("sync-proof over the real tree exited %d — the refusals above prove nothing "+
			"unless the define also lets a real subject through:\n%s", code, out)
	}
}

// #278's narrowing knobs. `make test RUN=<pattern>` and `PKG=<pattern>` are
// the same hazard inside the everyday target: `go test -run` exits 0 printing
// "[no tests to run]" over a pattern that matches nothing, and `go list` over
// a package pattern that matches nothing leaves the set empty. Both are
// checked fail-closed in the recipe and neither refusal was driven by a test.
func TestMakeTestRefusesANarrowingKnobThatSelectsNothing(t *testing.T) {
	needBinary(t, "make")
	needBinary(t, "go")
	// Every vector is scoped to the module's smallest package, so the guards
	// are driven without paying for a whole-module enumeration or race build.
	for _, v := range []struct {
		label string
		args  []string
		names string
	}{
		{"RUN matching no test", []string{"PKG=./internal/marker", "RUN=TestNoSuchTestNameAtAllZZZ"}, "matched no test in the selected packages"},
		{"PKG matching no package", []string{"PKG=./no/such/package"}, "matched no packages in this module"},
	} {
		code, out := runMake(t, "", append([]string{"test"}, v.args...)...)
		if code == 0 {
			t.Errorf("make test with %s exited 0 — an empty selection reads as a green run over "+
				"the whole suite:\n%s", v.label, out)
			continue
		}
		if !strings.Contains(out, v.names) {
			t.Errorf("make test with %s exited %d but never named %q as the reason:\n%s",
				v.label, code, v.names, out)
		}
	}

	// And a knob that DOES select is not refused — otherwise a `test` target
	// that rejected every narrowing would pass the two arms above. The package
	// is the smallest in the module, so the race build stays cheap.
	if code, out := runMake(t, "", "test", "PKG=./internal/marker", "RUN=Test"); code != 0 {
		t.Errorf("make test PKG=./internal/marker RUN=Test exited %d — the refusals above prove "+
			"nothing unless a real selection still runs:\n%s", code, out)
	}
}

// makeTargetNames returns every target defined in the Makefile. Target-scoped
// variable lines (`verify: override PKG :=`) name the same target and collapse
// into it.
func makeTargetNames(mk string) []string {
	def := regexp.MustCompile(`^([a-z][a-z0-9-]*):`)
	seen := map[string]bool{}
	var out []string
	for _, l := range strings.Split(mk, "\n") {
		m := def.FindStringSubmatch(l)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}

func phonyNames(mk string) []string {
	for _, l := range strings.Split(mk, "\n") {
		if strings.HasPrefix(l, ".PHONY:") {
			return strings.Fields(strings.TrimPrefix(l, ".PHONY:"))
		}
	}
	return nil
}

// #317. Both directions. A defined target missing from .PHONY is a FILE
// target: `touch panel-proof` makes `make panel-proof` print "is up to date"
// and exit 0 with the recipe unrun, inside `make verify`. A .PHONY name with
// no target is the reverse rot — a declaration about something that no longer
// exists, which is how the first ten went unnoticed.
func TestEveryMakeTargetIsDeclaredPhony(t *testing.T) {
	mk := readMakefile(t)
	targets := makeTargetNames(mk)
	if len(targets) == 0 {
		t.Fatal("no targets parsed out of the Makefile — refusing to report a pass over zero")
	}
	phony := map[string]bool{}
	for _, n := range phonyNames(mk) {
		phony[n] = true
	}
	defined := map[string]bool{}
	for _, n := range targets {
		defined[n] = true
		if !phony[n] {
			t.Errorf("target %q is not in .PHONY — a file of that name in the repo root makes "+
				"make report \"up to date\" and exit 0 with the recipe unrun", n)
		}
	}
	for n := range phony {
		if !defined[n] {
			t.Errorf(".PHONY names %q, which is not a target in this Makefile — a stale declaration", n)
		}
	}
}
