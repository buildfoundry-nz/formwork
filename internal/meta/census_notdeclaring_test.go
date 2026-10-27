// census_notdeclaring_test.go — scope.not_declaring in the escape-hatch census.
//
// A file that declares one of a rule's not_declaring markers leaves that rule's
// evaluation whole: the rule never runs on it, so no finding exists to count.
// It is the one exemption channel left that removes code from a rule, and the
// census printed nothing for it at all, although scopedeclare.go promised it
// was "reported with live match counts rather than silently". Each marker now
// reports the in-scope files it removed and how many of those the rule would
// have fired on, which is what separates an exemption doing work from a fossil.
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

// Two live markers on one rule — one removing a file the rule fires on, one
// removing a file it does not — plus a marker no file declares.
func notDeclaringRepo() map[string]string {
	return map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: forbidden-pattern\n" +
			"    scope:\n" +
			"      include: ['**/*.go']\n" +
			"      not_declaring: ['LIVE-HOME', 'FOSSIL-HOME', 'DEAD-HOME']\n" +
			"    params: {pattern: 'Ghost'}\n" +
			"    cure: \"drop it\"\n",
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"src.go": "package p\n",
		// Declares a marker AND would fire: the marker is load-bearing.
		"live.go": "package p // LIVE-HOME\nvar Ghost = 1\n",
		// Declares a marker and would NOT fire: the marker protects nothing.
		"fossil.go": "package p // FOSSIL-HOME\n",
		// Declares a marker but sits outside the rule's scope: not removed by it.
		"notes.md": "LIVE-HOME\n",
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
	live := censusLine(t, out, "LIVE-HOME")
	if !strings.Contains(live, "1 file(s) removed") || !strings.Contains(live, "1 would fire") {
		t.Errorf("the live marker removes one in-scope file the rule fires on (notes.md is out of scope):\n  %s", live)
	}
	fossil := censusLine(t, out, "FOSSIL-HOME")
	if !strings.Contains(fossil, "1 file(s) removed") || !strings.Contains(fossil, "0 would fire") {
		t.Errorf("the fossil marker removes a file the rule would not fire on and must read 0:\n  %s", fossil)
	}
	dead := censusLine(t, out, "DEAD-HOME")
	if !strings.Contains(dead, "0 file(s) removed") {
		t.Errorf("a marker no file declares removes nothing and must say 0:\n  %s", dead)
	}
}

// The dead-entry check asks about EACH marker. It used to ask whether any of the
// rule's markers matched, so DEAD-HOME read as live because LIVE-HOME was.
func TestLintReportsADeadNotDeclaringMarkerBesideALiveSibling(t *testing.T) {
	_, out := lint(t, notDeclaringRepo())
	if !strings.Contains(out, `scope.not_declaring "DEAD-HOME" is declared by no file`) {
		t.Fatalf("a dead marker must be reported even when a sibling marker is live:\n%s", out)
	}
	if strings.Contains(out, `scope.not_declaring "LIVE-HOME" is declared by no file`) {
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
			"      not_declaring: ['GENERATED']\n" +
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
	censusLine(t, out, "FOSSIL-HOME") // the census ran; a silent one would pass below
	if strings.Contains(out, "1 would fire") || strings.Contains(out, "0 would fire") {
		t.Fatalf("an unreadable in-scope file makes every would-fire count a guess:\n%s", out)
	}
}
