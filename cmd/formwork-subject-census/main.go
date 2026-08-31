// formwork-subject-census — a name-anchored rule must still be able to SEE its
// subject. An anchor that matches no call anywhere in the rule's scope is a
// gate that reports OK because it has gone blind, not because the invariant
// holds.
//
// Usage: go run -C tools/formwork-subject-census . <repo-root>
//
// Product enforcement is formwork type:command
// (.formwork/rules/call-anchor-sees-a-subject.yaml) with origin on this file.
// Exit 0 = every live anchor resolves and no stale debt entries, 1 = offenders
// or staleness listed, 2 = usage/env error.
//
// # The hole this closes, and the half that was already closed
//
// Six formwork rule types select their subject by NAME, skip whatever the name
// does not match, and then report nothing — so an empty anchor set is
// indistinguishable from full compliance (#10517, #15714). The ENGINE closes
// three of them: formwork's internal/rules/anchor.go AnchorProbe is wired
// into go/call-order-in-func, go/per-func-count-relation and
// dart/method-delegates, and emits a scope-wide verdict when the anchor matches
// nothing. formwork-anchor-census closes the one gap the engine left there, by
// making per-func-count-relation's optional `require_symbol:` mandatory.
//
// go/call-confined-to-func-name and go/guard-precedes-call carry NO probe.
// Verified by execution on develop: renaming `LockSQL` and
// `InsertOrAdoptActive` in the real tree — an ordinary refactor — left
// page-metrics-lock-sql-confined, page-metrics-advisory-after-page-row and
// extraction-dispatch-requires-entitlement reporting OK on a whole-tree run,
// with rules_matching_no_files: 0 confirming they had scanned and simply seen
// nothing. The last of those three is the entitlement gate on extraction
// dispatch, retired in total silence.
//
// # Why the question is "is there a CALL", not "is there a declaration"
//
// Both analyzers match their anchor against a call SELECTOR — the text at the
// call site (goast/analyzers.go: `call.selector`). So the faithful question is
// whether any in-scope call still matches, and asking it on the call plane
// removes a whole false-positive class that a declaration search cannot avoid:
// `MethodFunc` and `.GenerateContent` are declared in chi and the genai SDK,
// outside the repo, where no declaration index reaches — but their call sites
// are right here in the tree, so on the call plane they resolve as live and
// need no module resolution at all.
//
// A dead anchor cannot distinguish "renamed" from "feature deleted", and does
// not need to: in both cases the rule now guards nothing, and both cures — re-
// anchor it, or retire it — are named in the rule's cure text.
//
// # What is deliberately NOT flagged
//
// The BAN IDIOM. A call-confined arm whose `allowed_func` can match no func
// name at all ("this helper must have no callers anywhere") is satisfied
// PRECISELY by the symbol matching nothing — zero calls is its compliant end
// state, not its blind spot. Flagging those would fire on 4 of the 31
// call-confined arms in the corpus for being correct. This is the same reason
// the engine takes per-func-count-relation's require_symbol as an author
// declaration rather than inferring it.
//
// GATE-TREE-SCOPED arms, on the rationale formwork-vacuity-census's
// detectorWitnesses already applies: a rule whose whole subject is the gate
// tree can only ever be witnessed by gate sources, and calling that vacuous
// would condemn every gate-about-a-gate rule in the corpus.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/buildfoundry-nz/formwork/internal/census"
	"gopkg.in/yaml.v3"
)

// stats is the census part of the run, printed on every invocation so the
// ratchet's blast radius is visible in CI logs without a separate query.
type stats struct {
	files    int
	arms     int
	anchored int // arms of a covered type that were actually resolved
	waived   int // ban-idiom + gate-tree arms
}

// detect scans root/.formwork/rules/*.yaml and returns every covered arm whose
// anchor resolves to no in-scope call, plus corpus stats. It reads the corpus
// and the product tree ONLY; allowlist reconciliation is main's job, so tests
// exercise detection against bare fixture trees.
func detect(root string) ([]census.Finding, stats, error) {
	var st stats
	var out []census.Finding
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, st, err
	}
	sort.Strings(files)
	// One selector index per distinct scope, not per arm: the corpus reuses a
	// handful of scopes across many arms, and a fresh whole-tree parse for each
	// would multiply the walk by the arm count.
	cache := map[string]map[string]bool{}
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
			typ := mappingScalar(arm, "type")
			param, covered := anchorParam[typ]
			if !covered {
				continue
			}
			params := mappingValue(arm, "params")
			pattern := mappingScalar(params, param)
			if pattern == "" {
				continue
			}
			scope := mappingValue(arm, "scope")
			includes := seqStrings(mappingValue(scope, "include"))
			excludes := seqStrings(mappingValue(scope, "exclude"))
			if gateTreeScoped(includes) || banIdiom(typ, params) {
				st.waived++
				continue
			}
			key := fmt.Sprint(includes, "|", excludes)
			sel, ok := cache[key]
			if !ok {
				sel, err = callSelectors(root, includes, excludes)
				if err != nil {
					return nil, st, err
				}
				cache[key] = sel
			}
			st.anchored++
			live, err := anchorLive(pattern, sel)
			if err != nil {
				return nil, st, fmt.Errorf("%s: %s: %w", rel, mappingScalar(arm, "id"), err)
			}
			if live {
				continue
			}
			out = append(out, census.Finding{
				File: rel,
				Line: mappingKeyLine(arm, "type"),
				Arm:  mappingScalar(arm, "id"),
			})
		}
	}
	return out, st, nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: formwork-subject-census <repo-root>")
		os.Exit(2)
	}
	root := os.Args[1]
	flags, st, err := detect(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "formwork-subject-census:", err)
		os.Exit(2)
	}
	fmt.Printf("census: %d rule file(s), %d arm(s), %d anchor(s) resolved, %d waived\n",
		st.files, st.arms, st.anchored, st.waived)
	debt, err := census.ReadDebtList(filepath.Join(root, ".formwork", "allowlists", "blind-call-anchors.txt"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "formwork-subject-census:", err)
		os.Exit(2)
	}
	problems := census.Reconcile(os.Stdout, "call-anchor-sees-a-subject", flags, debt,
		"the anchor matches no call in scope: the rule can no longer see its subject")
	if problems > 0 {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// corpus walking — shared shape with formwork-anchor-census
// ---------------------------------------------------------------------------

func ruleArms(data []byte) ([]*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("no YAML document")
	}
	rules := mappingValue(doc.Content[0], "rules")
	if rules == nil || rules.Kind != yaml.SequenceNode {
		return nil, nil
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

func seqStrings(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}

func mustRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}
