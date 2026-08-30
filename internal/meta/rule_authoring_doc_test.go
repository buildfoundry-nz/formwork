// rule_authoring_doc_test.go — #111's citation obligation.
//
// The doctrine document's value is that every practice names the `formwork lint`
// check enforcing it, or says plainly that it is judgement with no enforcer. A
// citation naming a check that does not exist destroys that: the reader believes
// a practice is enforced when nothing is watching, which is worse than knowing
// it is unenforced.
//
// This repo has already been bitten by a documented path that never existed —
// `tools/parity` is annotated DESIGNED, NEVER BUILT in the design spec precisely
// because it was cited as real once before. A phantom lint check is the same
// defect aimed at the practices.
//
// Method: the check names are string literals at the emit() call sites, so there
// is no registry to ask. The test reads them out of the source rather than
// carrying a second copy — a duplicated list would drift, and the drift would be
// invisible. Same reasoning as scripts/claude-hooks-proof.sh reading the real
// hook out of settings.json with jq instead of restating the regex.
package meta_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// liveCheckNames reads every lint check name out of the emit() call sites.
func liveCheckNames(t *testing.T) map[string]bool {
	t.Helper()
	emitRE := regexp.MustCompile(`emit\(w, "([a-z][a-z0-9-]*)"`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range emitRE.FindAllStringSubmatch(string(b), -1) {
			names[m[1]] = true
		}
	}
	if len(names) < 8 {
		t.Fatalf("found only %d lint check names in the source (want >= 8) — the "+
			"scan is not finding the emit() sites, so the assertion below would "+
			"pass against almost nothing", len(names))
	}
	return names
}

func TestRuleAuthoringDocCitesOnlyRealLintChecks(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "rule-authoring.md"))
	if err != nil {
		t.Fatalf("the rule-authoring doctrine is part of the published tree: %v", err)
	}
	doc := string(b)
	live := liveCheckNames(t)

	// Only names in an **Enforcer:** line are citations. A check named in prose
	// elsewhere is discussion, not a claim about what enforces a practice.
	enforcerLine := regexp.MustCompile("(?m)^> \\*\\*Enforcers?[^\\n]*(?:\\n> [^\\n]*)*")
	backticked := regexp.MustCompile("`([a-z][a-z0-9-]*)`")

	blocks := enforcerLine.FindAllString(doc, -1)
	if len(blocks) == 0 {
		t.Fatal("no **Enforcer:** blocks found — either the document lost them or " +
			"this test's pattern no longer matches it; both are failures")
	}

	var phantom []string
	cited := 0
	for _, blk := range blocks {
		for _, m := range backticked.FindAllStringSubmatch(blk, -1) {
			name := m[1]
			// Only judge names that look like check names: hyphenated, and not
			// obviously a param or command word.
			if !strings.Contains(name, "-") {
				continue
			}
			cited++
			if !live[name] {
				phantom = append(phantom, name)
			}
		}
	}
	if cited == 0 {
		t.Fatal("no check names cited in any **Enforcer:** block — the assertion " +
			"would pass vacuously")
	}
	if len(phantom) > 0 {
		sort.Strings(phantom)
		names := make([]string, 0, len(live))
		for n := range live {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("docs/rule-authoring.md cites %d lint check(s) that do not exist: %s\n"+
			"live checks: %s\n"+
			"A citation naming a check nobody runs tells the reader a practice is "+
			"enforced when nothing is watching.",
			len(phantom), strings.Join(phantom, ", "), strings.Join(names, ", "))
	}
}
