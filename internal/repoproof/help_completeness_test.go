// help_completeness_test.go — `make help` lists every target it claims to.
//
// WHY THIS EXISTS. Three documents make a completeness claim about the help
// listing, and all three were false at 887acefa (#289):
//
//   - README.md: "`make help` lists every target with its one-line
//     description. It reads the Makefile, so unlike a list written out here it
//     cannot fall behind what `verify` actually depends on."
//   - tools/publication/public-AGENTS.md: the same sentence, which the OSS cut
//     materialises as the public tree's AGENTS.md.
//   - AGENTS.md: the more careful "lists every target carrying a `## name:`
//     comment — not every target (`all:` has none)".
//
// The help recipe generates its listing with an awk pattern over the Makefile,
// and that pattern is a character class. A class is a silent filter: a target
// whose name holds a character the class omits is dropped with no diagnostic,
// no exit code, and no gap in the output a reader could notice. `verify`
// depended on exactly such a target — `hooks-e2e-proof`, dropped because the
// class had no `0-9` — so the listing had fallen behind precisely the way the
// README says is structurally impossible.
//
// SO THIS GUARD MUST NOT SHARE THE RECIPE'S BLIND SPOT. It reads the name in a
// `## name:` comment as everything before the first colon, with no class at
// all, and it cross-checks that name against the Makefile's own rule lines.
// Any narrowing the recipe applies — today's missing digits, tomorrow's
// missing dot or plus — shows up here as a documented target the listing never
// printed. Reimplementing the recipe's awk would have reproduced the bug in
// the test and gone green over it.
//
// BOTH DIRECTIONS. Completeness alone is satisfied by a recipe that prints
// every line of the Makefile, so the third test pins the other edge: every
// name the listing prints is a real target and carries a description. And each
// test refuses to run on an empty parse, because "no verify prerequisites
// found" would otherwise be an all-quantifier over nothing.
//
// THE PUBLIC CUT COMES FREE. internal/publication/cut.go rewrites the verify
// prerequisite line and strips dropped targets, but never touches the help
// recipe — the cut ships this awk byte-for-byte. A listing that is complete
// over a superset of the cut's targets is complete over the cut's.
package repoproof_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A doc comment's target name is everything between `## ` and the first colon.
// Deliberately class-free: see the file comment. `##@ Section` headers carry no
// colon and so cannot match, and neither can a wrapped continuation line.
var docCommentLine = regexp.MustCompile(`^## ([^:]+):`)

// A rule line: a name at column 0 followed by a colon. Also class-free, and for
// the same reason — a class here would decide that a target whose name holds an
// unexpected character is not a target, which is the recipe's bug wearing a
// different hat. Recipe lines are excluded by the tab, comments by the `#`, and
// `NAME := value` by the space; the one case RE2's missing negative lookahead
// cannot express, `NAME:=value`, is the explicit check below.
var ruleLine = regexp.MustCompile(`^([^:=#[:space:]]+):`)

// makefileFacts is what the Makefile says about itself, parsed independently of
// how the help recipe reads it.
type makefileFacts struct {
	targets       map[string]bool   // every name defined by a rule line
	documented    map[string]string // target name -> the description in its `## name:` comment
	verifyPrereqs []string          // the prerequisites of the `verify` target
}

func readMakefileFacts(t *testing.T) makefileFacts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("cannot read the Makefile this gate judges: %v", err)
	}
	return parseMakefile(string(data))
}

// parseMakefile is separated from the read so the parser's own discriminations
// — the ones the real Makefile happens not to exercise today — can be put under
// a test instead of taken on trust.
func parseMakefile(text string) makefileFacts {
	f := makefileFacts{targets: map[string]bool{}, documented: map[string]string{}}

	lines := strings.Split(text, "\n")
	for _, l := range lines {
		if m := ruleLine.FindStringSubmatch(l); m != nil {
			rest := l[len(m[0]):]
			if strings.HasPrefix(rest, "=") { // NAME:=value is an assignment
				continue
			}
			if strings.HasPrefix(m[1], ".") { // .PHONY and friends are directives
				continue
			}
			f.targets[m[1]] = true
		}
	}
	for _, l := range lines {
		m := docCommentLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name := m[1]
		// Only names that are also rule lines: a `## Note: ...` sentence in the
		// prose above a target documents nothing the listing owes an entry to.
		if !f.targets[name] {
			continue
		}
		f.documented[name] = strings.TrimSpace(l[len(m[0]):])
	}

	// The prerequisite list is the one `verify:` line that is not a
	// target-scoped `override` assignment. Same discrimination cut.go makes.
	for _, l := range lines {
		if strings.HasPrefix(l, "verify:") && !strings.Contains(l, "override") {
			f.verifyPrereqs = append(f.verifyPrereqs, strings.Fields(strings.TrimPrefix(l, "verify:"))...)
		}
	}
	return f
}

// helpEntry is one printed line of the listing: the target name and the
// description beside it.
type helpEntry struct {
	name        string
	description string
}

// runHelp runs the real recipe. Nothing here reimplements it — the point of the
// gate is to judge the shipped output.
func runHelp(t *testing.T) []helpEntry {
	t.Helper()
	needBinary(t, "make")
	cmd := exec.Command("make", "help")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`make help` did not run: %v\n%s", err, out)
	}
	var entries []helpEntry
	for _, l := range strings.Split(string(out), "\n") {
		// Section headers print at column 0; target entries are indented.
		if !strings.HasPrefix(l, "  ") {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) == 0 {
			continue
		}
		entries = append(entries, helpEntry{
			name:        fields[0],
			description: strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), fields[0])),
		})
	}
	if len(entries) == 0 {
		t.Fatal("`make help` printed no target entries at all — this gate cannot answer against an empty listing")
	}
	return entries
}

func helpNames(entries []helpEntry) map[string]bool {
	names := map[string]bool{}
	for _, e := range entries {
		names[e.name] = true
	}
	return names
}

func sortedMissing(want []string, have map[string]bool) []string {
	seen := map[string]bool{}
	var missing []string
	for _, n := range want {
		if have[n] || seen[n] {
			continue
		}
		seen[n] = true
		missing = append(missing, n)
	}
	sort.Strings(missing)
	return missing
}

// TestHelpListsEveryVerifyPrerequisite pins the README and public-AGENTS claim
// that the listing "cannot fall behind what `verify` actually depends on".
func TestHelpListsEveryVerifyPrerequisite(t *testing.T) {
	facts := readMakefileFacts(t)
	if len(facts.verifyPrereqs) == 0 {
		t.Fatal("parsed no prerequisites from the `verify:` line — refusing to report a pass over an empty set")
	}
	printed := helpNames(runHelp(t))
	if missing := sortedMissing(facts.verifyPrereqs, printed); len(missing) > 0 {
		t.Errorf("`make help` omits %d of verify's %d prerequisites: %s\n"+
			"README.md and tools/publication/public-AGENTS.md both say the listing "+
			"cannot fall behind what verify depends on. Every name above is a prerequisite "+
			"the listing never printed, so the claim is false.",
			len(missing), len(facts.verifyPrereqs), strings.Join(missing, ", "))
	}
}

// TestHelpListsEveryDocumentedTarget pins AGENTS.md's more careful claim: every
// target carrying a `## name:` comment appears, `all:` being excluded because it
// carries none.
func TestHelpListsEveryDocumentedTarget(t *testing.T) {
	facts := readMakefileFacts(t)
	if len(facts.documented) == 0 {
		t.Fatal("parsed no `## name:` comments from the Makefile — refusing to report a pass over an empty set")
	}
	var documented []string
	for name := range facts.documented {
		documented = append(documented, name)
	}
	sort.Strings(documented)

	printed := helpNames(runHelp(t))
	if missing := sortedMissing(documented, printed); len(missing) > 0 {
		t.Errorf("`make help` omits %d of the %d targets carrying a `## name:` comment: %s\n"+
			"AGENTS.md says the listing carries every one of them. The help recipe filters "+
			"target names through a character class (Makefile, the awk in the `help` target); "+
			"a name holding a character that class omits is dropped silently.",
			len(missing), len(documented), strings.Join(missing, ", "))
	}
}

// TestHelpPrintsOnlyRealTargetsWithDescriptions pins the other edge. Without
// it, a recipe that printed every line of the Makefile would satisfy both
// completeness tests above while telling a reader nothing true.
func TestHelpPrintsOnlyRealTargetsWithDescriptions(t *testing.T) {
	facts := readMakefileFacts(t)
	if len(facts.targets) == 0 {
		t.Fatal("parsed no rule lines from the Makefile — refusing to report a pass over an empty set")
	}
	for _, e := range runHelp(t) {
		if !facts.targets[e.name] {
			t.Errorf("`make help` lists %q, which no rule line in the Makefile defines", e.name)
			continue
		}
		if e.description == "" {
			t.Errorf("`make help` lists %q with no description — README.md promises "+
				"every target with its one-line description", e.name)
		}
		if want := facts.documented[e.name]; want != "" && want != e.description {
			t.Errorf("`make help` describes %q as %q, but its `## %s:` comment reads %q",
				e.name, e.description, e.name, want)
		}
	}
}

// TestHelpGuardParserReadsNamesTheRecipeClassWouldDrop puts the guard's own
// parser under test rather than trusting it.
//
// WHY. This guard's only value is that it does NOT filter names the way the
// help recipe does — a class-free read is the whole mechanism. But three of the
// parser's discriminations are unexercised by the real Makefile (no target
// today is named `NAME:=value`, none carries a character outside `[a-z0-9-]`,
// and every `## name:` comment names a real target), so a regression in any of
// them would leave both completeness tests green while the guard quietly went
// blind — the same failure mode as the bug it exists to catch.
func TestHelpGuardParserReadsNamesTheRecipeClassWouldDrop(t *testing.T) {
	const src = "" +
		".PHONY: all synth-e2e\n" +
		"CACHE:=on\n" +
		"FLAG ?= off\n" +
		"\n" +
		"## Note: this sentence names no target and owes the listing nothing\n" +
		"\n" +
		"## synth-e2e: a name with a digit, which the recipe's class drops\n" +
		"## a wrapped continuation line, carrying no colon\n" +
		"synth-e2e:\n" +
		"\t@echo run\n" +
		"\n" +
		"## synth.plus+odd: a name outside any plausible class\n" +
		"synth.plus+odd:\n" +
		"\t@echo run\n" +
		"\n" +
		"## undocumented-by-nobody: has a comment and a rule\n" +
		"undocumented-by-nobody:\n" +
		"\t@echo run\n" +
		"\n" +
		"bare-target:\n" +
		"\t@echo no comment, so the listing owes it nothing\n" +
		"\n" +
		"verify: override PKG :=\n" +
		"verify: synth-e2e synth.plus+odd\n"

	f := parseMakefile(src)

	for _, name := range []string{"synth-e2e", "synth.plus+odd", "undocumented-by-nobody", "bare-target"} {
		if !f.targets[name] {
			t.Errorf("parser did not see %q as a target — it would then excuse the listing for dropping it", name)
		}
	}
	for _, notATarget := range []string{"CACHE", "FLAG", ".PHONY", "Note"} {
		if f.targets[notATarget] {
			t.Errorf("parser read %q as a target; it is an assignment, a directive or prose", notATarget)
		}
	}

	wantDocumented := map[string]string{
		"synth-e2e":              "a name with a digit, which the recipe's class drops",
		"synth.plus+odd":         "a name outside any plausible class",
		"undocumented-by-nobody": "has a comment and a rule",
	}
	if len(f.documented) != len(wantDocumented) {
		t.Errorf("parser found %d documented targets, want %d: %v", len(f.documented), len(wantDocumented), f.documented)
	}
	for name, want := range wantDocumented {
		if got := f.documented[name]; got != want {
			t.Errorf("description for %q: got %q, want %q", name, got, want)
		}
	}
	if _, ok := f.documented["Note"]; ok {
		t.Error("parser turned a `## Note:` prose line into a documented target")
	}
	if _, ok := f.documented["bare-target"]; ok {
		t.Error("parser invented a doc comment for a target that carries none")
	}

	wantPrereqs := []string{"synth-e2e", "synth.plus+odd"}
	if strings.Join(f.verifyPrereqs, " ") != strings.Join(wantPrereqs, " ") {
		t.Errorf("verify prerequisites: got %v, want %v — the `override` line must not be read as a prerequisite list",
			f.verifyPrereqs, wantPrereqs)
	}
}
