// builtin_skip_test.go — #56.
//
// The engine-level skip set (.git, .formwork, by basename at any depth) is
// correct and load-bearing: .formwork/fixtures holds deliberately-broken fire
// fixtures whose job is to contain violations, so scanning them would make every
// rule fail on its own test data and `formwork test` could not exist. This is
// not about removing it.
//
// It was UNAUDITABLE. It appears in no rule's scope.exclude and cannot — it is
// an engine precondition, not a per-rule choice — and it was the one exemption
// channel absent from the escape-hatch census unless the operator happened to
// declare an unrelated scan.ignore. Downstream, 28 rules declared coverage the
// scanner never gives, and the repo's own docs stated a ban "anywhere, no
// exemptions" that was measurably false for .formwork/.
package meta_test

import (
	"strings"
	"testing"
)

const plainRule = "rules:\n" +
	"  - id: no-ghost\n" +
	"    type: forbidden-pattern\n" +
	"    scope: {include: ['**/*.go']}\n" +
	"    params: {pattern: 'Ghost'}\n" +
	"    cure: \"drop it\"\n"

func plainRepo(extra map[string]string) map[string]string {
	base := map[string]string{
		".formwork/formwork.yaml":                 "version: 1\n",
		".formwork/rules/r.yaml":                  plainRule,
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"src.go": "package p\n",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// The census must name the engine skip set on EVERY run. It used to appear only
// when scan.ignore or scan.gitignore was declared, so a repo that declared
// neither — the common case — never learned the skip existed.
func TestCensusAlwaysNamesTheEngineSkipSet(t *testing.T) {
	_, out := lint(t, plainRepo(nil))
	if !strings.Contains(out, "never scans directories named") {
		t.Fatalf("the engine skip set must be named without any scan.ignore declared:\n%s", out)
	}
	// Named from the set itself, not from prose that can drift away from it.
	for _, d := range []string{".git", ".formwork"} {
		if !strings.Contains(out, d) {
			t.Fatalf("the census must name %q as a skipped directory:\n%s", d, out)
		}
	}
}

// empty-scope already reports a rule matching nothing. When the reason is that
// every include glob is rooted under a directory the engine never scans, saying
// so is the difference between "fix your glob" and an afternoon wondering why a
// correct-looking glob matches nothing.
func TestEmptyScopeExplainsAScopeRootedUnderASkippedDirectory(t *testing.T) {
	files := plainRepo(nil)
	files[".formwork/rules/r.yaml"] = "rules:\n" +
		"  - id: fixture-police\n" +
		"    type: forbidden-pattern\n" +
		"    scope: {include: ['.formwork/**']}\n" +
		"    params: {pattern: 'Ghost'}\n" +
		"    cure: \"drop it\"\n"
	files[".formwork/fixtures/fixture-police/fire-1/a.go"] = "package p // Ghost want: fixture-police\n"
	files[".formwork/fixtures/fixture-police/pass-1/b.go"] = "package p\n"
	delete(files, ".formwork/fixtures/no-ghost/fire-1/a.go")
	delete(files, ".formwork/fixtures/no-ghost/pass-1/b.go")

	_, out := lint(t, files)
	if !strings.Contains(out, "fixture-police") {
		t.Fatalf("precondition: the rule should be reported as empty:\n%s", out)
	}
	if !strings.Contains(out, "every include is rooted") {
		t.Fatalf("empty-scope must say WHY the scope is empty when every include is "+
			"rooted under a skipped directory:\n%s", out)
	}
}

// The narrowing. A rule with an ordinary glob that happens to match nothing gets
// the plain message — the skip-set explanation must not be attached to every
// empty scope, or it becomes noise that means nothing.
func TestEmptyScopeDoesNotBlameTheSkipSetForAnOrdinaryEmptyScope(t *testing.T) {
	files := plainRepo(nil)
	files[".formwork/rules/r.yaml"] = "rules:\n" +
		"  - id: no-ghost\n" +
		"    type: forbidden-pattern\n" +
		"    scope: {include: ['nonexistent/**']}\n" +
		"    params: {pattern: 'Ghost'}\n" +
		"    cure: \"drop it\"\n"
	_, out := lint(t, files)
	if !strings.Contains(out, "no-ghost: scope matches no files") {
		t.Fatalf("precondition: the rule should be reported as empty:\n%s", out)
	}
	// Deliberately NOT "never scans": the unconditional context line above the
	// escape-hatch block contains that phrase on every run, so it cannot tell
	// the two cases apart. The per-rule explanation is what must be absent.
	if strings.Contains(out, "every include is rooted") {
		t.Fatalf("an ordinary empty scope must not be blamed on the skip set:\n%s", out)
	}
}
