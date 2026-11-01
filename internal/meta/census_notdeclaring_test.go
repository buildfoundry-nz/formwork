// census_notdeclaring_test.go — scope.not_declaring in the escape-hatch census.
//
// Each place that declares one of a rule's not_declaring markers opens a home
// declaration the engine masks before the rule reads the file, so no finding
// exists inside it to count. The census printed nothing for the key at all,
// although scopedeclare.go promised it was "reported with live match counts
// rather than silently". Each marker now reports the home declarations it
// removed and how many of those the rule would have fired inside, which is
// what separates a sanctioned site doing work from a fossil.
package meta_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
)

// Two live markers on one rule — one opening a home the rule fires inside, one
// opening a home it does not — plus a marker nothing declares.
func notDeclaringRepo() map[string]string {
	return map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: forbidden-pattern\n" +
			"    scope:\n" +
			"      include: ['**/*.go']\n" +
			"      not_declaring: [{marker: 'func LiveHome(', holders: 1}, {marker: 'func FossilHome(', holders: 1}, {marker: 'func DeadHome(', holders: 1}]\n" +
			"    params: {pattern: 'Ghost'}\n" +
			"    cure: \"drop it\"\n",
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"src.go": "package p\n",
		// Opens a home the rule would fire inside: the marker is load-bearing.
		"live.go": "package p\n\nfunc LiveHome() {\n\tGhost()\n}\n",
		// Opens a home the rule would NOT fire inside: the marker protects nothing.
		"fossil.go": "package p\n\nfunc FossilHome() {}\n",
		// Declares a marker but sits outside the rule's scope: not removed by it.
		"notes.md": "func LiveHome(\n",
	}
}

func censusLine(t *testing.T, out, marker string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "scope.not_declaring: "+strconv.Quote(marker)) {
			return l
		}
	}
	t.Fatalf("no scope.not_declaring census line for %q in:\n%s", marker, out)
	return ""
}

func TestCensusReportsWhatEachNotDeclaringMarkerRemoved(t *testing.T) {
	_, out := lint(t, notDeclaringRepo())
	live := censusLine(t, out, "func LiveHome(")
	if !strings.Contains(live, "1 declaration(s) removed") || !strings.Contains(live, "1 would fire") {
		t.Errorf("the live marker removes one in-scope file the rule fires on (notes.md is out of scope):\n  %s", live)
	}
	fossil := censusLine(t, out, "func FossilHome(")
	if !strings.Contains(fossil, "1 declaration(s) removed") || !strings.Contains(fossil, "0 would fire") {
		t.Errorf("the fossil marker removes a file the rule would not fire on and must read 0:\n  %s", fossil)
	}
	dead := censusLine(t, out, "func DeadHome(")
	if !strings.Contains(dead, "0 declaration(s) removed") {
		t.Errorf("a marker no file declares removes nothing and must say 0:\n  %s", dead)
	}
}

// The dead-entry check asks about EACH marker. It used to ask whether any of the
// rule's markers matched, so DEAD-HOME read as live because LIVE-HOME was.
func TestLintReportsADeadNotDeclaringMarkerBesideALiveSibling(t *testing.T) {
	_, out := lint(t, notDeclaringRepo())
	if !strings.Contains(out, `scope.not_declaring "func DeadHome(" is declared by no file`) {
		t.Fatalf("a dead marker must be reported even when a sibling marker is live:\n%s", out)
	}
	if strings.Contains(out, `scope.not_declaring "func LiveHome(" is declared by no file`) {
		t.Fatalf("a declared marker was reported dead:\n%s", out)
	}
}

// required-pattern emits its verdict in Finalize, so no per-file call can
// produce its finding: a would-fire count for it would be invented.
func TestCensusDoesNotInventAWouldFireCountForAFinalizerRule(t *testing.T) {
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: needs-header\n" +
			"    type: required-pattern\n" +
			"    scope:\n" +
			"      include: ['**/*.go']\n" +
			"      not_declaring: [{marker: GENERATED, holders: 1}]\n" +
			"    params: {pattern: 'LICENSE'}\n" +
			"    cure: \"add it\"\n",
		".formwork/fixtures/needs-header/fire-1/a.go": "package p\n",
		".formwork/fixtures/needs-header/pass-1/b.go": "package p // LICENSE\n",
		"src.go": "package p // LICENSE\n",
		"gen.go": "package p // GENERATED\n",
	})
	line := censusLine(t, out, "GENERATED")
	if !strings.Contains(line, "would fire: unknown") {
		t.Fatalf("a Finalize-verdict rule cannot answer per file; the census must say so:\n  %s", line)
	}
}

// An unreadable file cannot be judged either way. Counting it as "would not
// fire" points toward "this marker is a fossil", the direction that hides an
// exemption doing work.
func TestCensusDoesNotCountAnUnreadableFileEitherWay(t *testing.T) {
	skipUnlessChmodEnforced(t)
	root := writeRepo(t, notDeclaringRepo())
	if err := os.Chmod(filepath.Join(root, "live.go"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "live.go"), 0o644) })
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	_, _ = meta.Lint(cfg, root, &sb, false, false)
	out := sb.String()
	censusLine(t, out, "func FossilHome(") // the census ran; a silent one would pass below
	if strings.Contains(out, "1 would fire") || strings.Contains(out, "0 would fire") {
		t.Fatalf("an unreadable in-scope file makes every would-fire count a guess:\n%s", out)
	}
}

// A credential no in-scope line holds sanctions nothing. The check leaves the
// zero count alone (a pruned tree holds none of a repository's homes), so lint
// reports it, beside a live credential it must not report.
func TestLintReportsACredentialNoLineHolds(t *testing.T) {
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.go']}\n" +
			"    params:\n" +
			"      pattern: 'Ghost'\n" +
			"      require_absent:\n" +
			"        - {credential: '^func LiveHome\\(', holders: 1}\n" +
			"        - {credential: '^func DeadHome\\(', holders: 1}\n" +
			"    cure: \"drop it\"\n",
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"live.go": "package p\n\nfunc LiveHome() {\n\tGhost()\n}\n",
	})
	if !strings.Contains(out, `credential "^func DeadHome\\(" is held by no line this rule covers`) {
		t.Fatalf("a credential nothing holds must be reported:\n%s", out)
	}
	if strings.Contains(out, `credential "^func LiveHome\\(" is held by no line`) {
		t.Fatalf("a held credential was reported dead:\n%s", out)
	}
}

// A production subject is counted in the census by the test files it leaves
// out, and lint refuses one that leaves out nothing.
func TestLintCountsAProductionSubjectAndRefusesADeadOne(t *testing.T) {
	rules := "rules:\n" +
		"  - id: live-prod\n    type: forbidden-pattern\n    scope: {include: ['**/*.go'], subject: production}\n    params: {pattern: 'Ghost'}\n    cure: \"drop it\"\n" +
		"  - id: dead-prod\n    type: forbidden-pattern\n    scope: {include: ['cmd/**/*.go'], subject: production}\n    params: {pattern: 'Ghost'}\n    cure: \"drop it\"\n"
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                      "version: 1\ntests: ['**/*_test.go']\n",
		".formwork/rules/r.yaml":                       rules,
		".formwork/fixtures/live-prod/fire-1/a.go":     "package p // Ghost want: live-prod\n",
		".formwork/fixtures/live-prod/pass-1/b.go":     "package p\n",
		".formwork/fixtures/dead-prod/fire-1/cmd/a.go": "package p // Ghost want: dead-prod\n",
		".formwork/fixtures/dead-prod/pass-1/cmd/b.go": "package p\n",
		"src.go":      "package p\n",
		"src_test.go": "package p\n",
		"cmd/main.go": "package main\n",
	})
	if !strings.Contains(out, "live-prod: scope.subject: production: 1 test file(s) left out") {
		t.Fatalf("the census must count what a production subject leaves out:\n%s", out)
	}
	if !strings.Contains(out, "dead-prod: scope.subject: production leaves out no file") {
		t.Fatalf("a production subject that leaves out nothing must be refused:\n%s", out)
	}
}
