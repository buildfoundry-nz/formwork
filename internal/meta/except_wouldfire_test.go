// except_wouldfire_test.go — #138's remaining half.
//
// The census already reports how many files each `except.paths` entry removed
// from a rule's evaluation. That is the first of the two numbers #138 asks for
// and it does not separate a live carve-out from a fossil: an entry that removes
// three files the rule would never have fired on reads exactly like one that
// removes the single file it would have.
//
// The second number is the one that ranks them — of the files this entry
// removed, how many would the rule have FIRED on. Zero means the entry is
// protecting nothing, the same verdict a typo'd scan.ignore glob gets from its
// `0 matches`.
//
// WHY IT CANNOT COME FROM THE FINDINGS, which is what makes this channel
// different from every other exemption. except.paths is a scope SUBTRACTION,
// not a suppression: config.Rule.Applies returns false, the rule never evaluates
// the file, and no finding — suppressed or otherwise — exists to count. The
// number has to be produced by evaluating the rule against the carved-out files
// on purpose.
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

// The fixture #138 specifies: two entries on one rule, one naming a file the
// rule fires on and one naming an in-scope file it does not.
func exceptRepo() map[string]string {
	return map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.go']}\n" +
			"    except: {paths: ['carved/live/**', 'carved/fossil/**']}\n" +
			"    params: {pattern: 'Ghost'}\n" +
			"    cure: \"drop it\"\n",
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"src.go": "package p\n",
		// Carved out AND would fire: the entry is load-bearing.
		"carved/live/x.go": "package p // Ghost\n",
		// Carved out and would NOT fire: the entry is protecting nothing.
		"carved/fossil/y.go": "package p\n",
	}
}

func TestCensusSeparatesALiveExceptEntryFromAFossil(t *testing.T) {
	_, out := lint(t, exceptRepo())
	var live, fossil string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(l, "carved/live/**"):
			live = l
		case strings.Contains(l, "carved/fossil/**"):
			fossil = l
		}
	}
	if live == "" || fossil == "" {
		t.Fatalf("both except.paths entries must be enumerated:\n%s", out)
	}
	if live == strings.Replace(fossil, "fossil", "live", 1) {
		t.Fatalf("the two entries render identically, so the census cannot tell a "+
			"load-bearing carve-out from one protecting nothing:\n  %s\n  %s", live, fossil)
	}
	if !strings.Contains(live, "1 would fire") {
		t.Errorf("the live entry removes a file the rule WOULD fire on and must say so:\n  %s", live)
	}
	if !strings.Contains(fossil, "0 would fire") {
		t.Errorf("the fossil entry removes a file the rule would not fire on and must "+
			"read 0, the way a typo'd scan.ignore glob reads 0 matches:\n  %s", fossil)
	}
}

// A whole-run rule cannot answer this per file — its verdict depends on the set
// it was given, so evaluating one carved-out file in isolation would invent a
// number. It must say it cannot answer rather than print a 0 that reads as
// "this entry protects nothing".
func TestCensusDoesNotInventAWouldFireCountForAWholeRunRule(t *testing.T) {
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: heavy-gate\n" +
			"    type: command\n" +
			"    fixture_exempt: \"needs a live cluster\"\n" +
			"    scope: {include: ['**/*.go']}\n" +
			"    except: {paths: ['carved/**']}\n" +
			"    params: {cmd: [bash, -c, \"true\"]}\n" +
			"    cure: \"drop it\"\n",
		"src.go":      "package p\n",
		"carved/x.go": "package p\n",
	})
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "carved/**") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("the entry must still be enumerated:\n%s", out)
	}
	if strings.Contains(line, "would fire") && !strings.Contains(line, "unknown") {
		t.Fatalf("a whole-run rule's verdict depends on the set it was given, so a "+
			"per-file would-fire count would be invented; say it cannot be answered:\n  %s", line)
	}
}

// lintMutating is `lint` with a hook between writing the tree and reading it,
// for the cases whose whole point is a file the lint run cannot read.
func lintMutating(t *testing.T, files map[string]string, mutate func(root string)) (int, string) {
	t.Helper()
	root := writeRepo(t, files)
	mutate(root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	devOptOutActive, _ := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	failed, err := meta.Lint(cfg, root, &sb, devOptOutActive, false)
	if err != nil {
		return failed, sb.String() + "\nLINT ERROR: " + err.Error()
	}
	return failed, sb.String()
}

// A Finalizer that is NOT an ErrFinalizer. The first version of this test used a
// `command` rule, which is an ErrFinalizer — so it passed with the Finalizer and
// whole-tree guards both deleted, and mutation caught that it proved neither.
// required-pattern is the honest subject: it accumulates across files and emits
// its verdict in Finalize, so no per-file call can produce its finding.
func TestCensusDoesNotInventAWouldFireCountForAFinalizerRule(t *testing.T) {
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: needs-header\n" +
			"    type: required-pattern\n" +
			"    scope: {include: ['**/*.go']}\n" +
			"    except: {paths: ['carved/**']}\n" +
			"    params: {pattern: 'LICENSE'}\n" +
			"    cure: \"add it\"\n",
		".formwork/fixtures/needs-header/fire-1/a.go": "package p\n",
		".formwork/fixtures/needs-header/pass-1/b.go": "package p // LICENSE\n",
		"src.go":      "package p // LICENSE\n",
		"carved/x.go": "package p\n",
	})
	line := exceptLine(t, out, "carved/**")
	if !strings.Contains(line, "unknown") {
		t.Fatalf("required-pattern emits its verdict in Finalize, so a per-file "+
			"would-fire count is invented; the census must say it cannot answer:\n  %s", line)
	}
}

// An unreadable carved-out file must not be counted as "would not fire". That
// error points the count toward "this entry is a fossil", which is the direction
// that gets an exemption deleted.
func TestCensusDoesNotCountAnUnreadableCarvedFileAsNotFiring(t *testing.T) {
	skipUnlessChmodEnforced(t)
	files := exceptRepo()
	_, out := lintMutating(t, files, func(root string) {
		if err := os.Chmod(filepath.Join(root, "carved", "live", "x.go"), 0o000); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "1 would fire") || strings.Contains(out, "0 would fire") {
		t.Fatalf("an unreadable carved-out file cannot be judged either way; a count "+
			"here is invented:\n%s", out)
	}
}

func exceptLine(t *testing.T, out, glob string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, glob) && strings.Contains(l, "except.paths") {
			return l
		}
	}
	t.Fatalf("no except.paths line for %q in:\n%s", glob, out)
	return ""
}
