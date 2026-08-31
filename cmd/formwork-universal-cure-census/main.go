// formwork-universal-cure-census — ratchet R3: no rule arm may pair an
// EXISTENTIAL detector with a UNIVERSAL cure.
//
// Usage: go run -C tools/formwork-universal-cure-census . <repo-root>
//
// Product enforcement is formwork type:command
// (.formwork/rules/exists-rule-cure-not-universal.yaml) with origin on this
// file. Exit 0 = no new mis-paired arms and no stale debt entries,
// 1 = offenders or staleness listed, 2 = usage/env error.
//
// # The hole
//
// The audit measured 396 "absence-only" rules: they prove they fail when the
// guarded thing is DELETED but stay green on realistic present-but-wrong
// violations, because `mode: exists` asserts only that the pattern appears
// somewhere in scope. Most existential rules are honest — their cure asks for
// presence, which is what the detector checks. The hole is the arm whose cure
// states a UNIVERSAL obligation (every / each / all of) while detection stays
// existential: "every route must call checkFoo" is not discharged by one route
// calling it, yet the arm goes green on the first compliant file and cannot
// see any violation after it.
//
// # The detector
//
// The engine's scanner skips .formwork (spec §11), so no declarative rule can
// read the rule corpus — that is why this is an external tool like the
// vacuity census, not a forbidden-pattern arm. Per rule file, an arm FLAGS
// when it carries `mode: exists` AND its cure TEXT contains a universal-
// obligation word (every / each / all of). Both halves are arm-local, and the
// universal word must sit in the cure block itself (the `cure:` line plus its
// folded continuation) — not in a nearby comment, not in a sibling arm's
// cure. A whole-file co-occurrence false-positives on multi-arm files where
// one arm is existential and a DIFFERENT arm's cure is universal (the
// api-factory-cmd-lifecycle-class shape, pinned by testdata/pass-3), and a
// proximity window false-positives on an honest cure parked next to a comment
// that says "each" (the entitlements-audit-10001 shape, pinned by
// testdata/pass-4).
//
// # Known debt
//
// First-run census (2026-07-28): 9 arms across 8 files trip, all pre-existing,
// all carried in .formwork/allowlists/exists-rule-cure-not-universal-files.txt
// rather than mass-rewritten in the commit that installed the ratchet. The
// list is SELF-CLEANING: an entry whose file stops tripping (cured or
// deleted) fails this census as stale, so curing an arm forces its entry out.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/census"
	"gopkg.in/yaml.v3"
)

var (
	universal = regexp.MustCompile(`(?i)\b(every|each|all\s+of)\b`)
	armID     = regexp.MustCompile(`^\s*- id:\s*(\S+)`)
	cureKey   = regexp.MustCompile(`^(\s*)cure:\s*(.*)$`)
)

// stats is the census part of the run, printed on every invocation so the
// ratchet's blast radius is visible in CI logs without a separate query.
type stats struct {
	files      int
	arms       int
	existsArms int
}

// cureHasUniversal reports whether the arm's cure TEXT states a universal
// obligation. Only the cure block counts — the `cure:` line's inline value
// plus any folded continuation (lines more indented than the key). A
// universal word in a nearby comment or another key is NOT the cure
// over-promising: pinning the detector to the block is what keeps the
// entitlements-audit-10001 shape (a comment's "each" beside an honest
// singular cure) a pass.
func cureHasUniversal(lines []string, start, end int) bool {
	for i := start; i < end; i++ {
		m := cureKey.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if universal.MatchString(m[2]) {
			return true
		}
		keyIndent := len(m[1])
		for j := i + 1; j < end; j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				continue // blank lines cannot end a YAML folded scalar
			}
			indent := len(l) - len(strings.TrimLeft(l, " \t"))
			if indent <= keyIndent || armID.MatchString(l) {
				break
			}
			if universal.MatchString(l) {
				return true
			}
		}
	}
	return false
}

// detect scans root/.formwork/rules/*.yaml and returns every arm that pairs
// `mode: exists` with a universal-obligation cure, plus corpus stats. It
// reads the corpus ONLY; allowlist reconciliation is main's job, so tests
// exercise detection against bare fixture trees.
//
// Arms are walked as yaml.Node mappings so rematerialised keys (id not first)
// still parse the way the engine does, and a comment is never a cure (#14091).
func detect(root string) ([]census.Finding, stats, error) {
	var st stats
	var out []census.Finding
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, st, err
	}
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, st, err
		}
		rel := filepath.ToSlash(mustRel(root, f))
		st.files++
		arms, err := ruleArms(data)
		if err != nil {
			return nil, st, fmt.Errorf("%s: %w", rel, err)
		}
		for _, arm := range arms {
			st.arms++
			params := mappingValue(arm, "params")
			if mappingScalar(params, "mode") != "exists" {
				continue
			}
			st.existsArms++
			if !universal.MatchString(mappingScalar(arm, "cure")) {
				continue
			}
			out = append(out, census.Finding{
				File: rel,
				Line: mappingKeyLine(params, "mode"),
				Arm:  mappingScalar(arm, "id"),
			})
		}
	}
	return out, st, nil
}

func ruleArms(data []byte) ([]*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("no YAML document")
	}
	root := doc.Content[0]
	rules := mappingValue(root, "rules")
	if rules == nil {
		return nil, fmt.Errorf("no rules key")
	}
	if rules.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("rules is not a sequence")
	}
	return rules.Content, nil
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func mappingScalar(n *yaml.Node, key string) string {
	v := mappingValue(n, key)
	if v == nil {
		return ""
	}
	return v.Value
}

func mappingKeyLine(n *yaml.Node, key string) int {
	if n == nil || n.Kind != yaml.MappingNode {
		return 0
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i].Line
		}
	}
	return 0
}

func mustRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: formwork-universal-cure-census <repo-root>")
		os.Exit(2)
	}
	root := os.Args[1]

	flags, st, err := detect(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "census:", err)
		os.Exit(2)
	}
	debtPath := filepath.Join(root, ".formwork", "allowlists", "exists-rule-cure-not-universal-files.txt")
	debt, err := census.ReadDebtList(debtPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "census: read known-debt list:", err)
		os.Exit(2)
	}

	fmt.Printf("exists-rule-cure census: %d rule files, %d arms, %d mode:exists arms\n",
		st.files, st.arms, st.existsArms)

	const why = "pairs mode: exists with a universal cure (every/each/all of) — an existential detector cannot discharge a universal obligation (audit R3)"
	if census.Reconcile(os.Stdout, "exists-rule-cure-not-universal", flags, debt, why) > 0 {
		os.Exit(1)
	}
}
