// formwork-anchor-census — no `go/per-func-count-relation` arm may go
// unanchored: it must declare either the funcs it applies to or the side of
// the relation that constrains.
//
// Usage: go run -C tools/formwork-anchor-census . <repo-root>
//
// Product enforcement is formwork type:command
// (.formwork/rules/count-relation-arm-is-anchored.yaml) with origin on this
// file. Exit 0 = no new unanchored arms and no stale debt entries, 1 =
// offenders or staleness listed, 2 = usage/env error.
//
// # The hole
//
// A name-anchored rule that skips whatever its anchor does not match, and then
// reports nothing, cannot tell an empty anchor set from full compliance. The
// rule passes its fixture, somebody renames the subject for an unrelated
// reason, and the invariant is retired with no signal — a gate that is green
// because it can no longer see its own subject, which is worse than a missing
// gate because the dashboard says the invariant is held (#10517).
//
// The ENGINE closes that for the anchors it can see. `go/call-order-in-func`
// and `dart/method-delegates` take their anchors as REQUIRED params, so an arm
// of those types cannot exist without one and the engine's scope-wide
// existence verdict covers every arm by construction — a census over them
// could never fire and would be vacuous. `go/per-func-count-relation` is the
// one type an author can leave genuinely unanchored: `funcs:` is optional, and
// an arm without it constrains only through its `left`/`right` selectors,
// where `count(left) <= count(right)` holds vacuously at 0 <= 0. Rename the
// left symbol and the arm goes silent exactly the way a renamed func does.
//
// The engine cannot infer the symbol assertion, which is why it is the opt-in
// `require_symbol:` and why this census exists to make the opt-in mandatory.
// Absence is sometimes the COMPLIANT state: the forbidden-call-in-func idiom
// (`funcs:` plus a banned `left:` and a never-matching `right:` sentinel at
// `relation: <=`) is satisfied precisely by `left` matching nothing. Which
// side constrains is the author's judgment, so it has to be declared.
//
// # The detector
//
// The engine's scanner skips .formwork (spec §11), so no declarative rule can
// read the rule corpus — that is why this is an external tool like the vacuity
// census and the universal-cure census, not a forbidden-pattern arm. Per rule
// file, an arm FLAGS when its `type:` is `go/per-func-count-relation` and its
// `params:` block carries neither `funcs:` nor `require_symbol:`. Both halves
// are arm-local, and the anchor keys must sit in the arm's own params block —
// not in a comment, not in a sibling arm — so the params block is walked
// explicitly rather than the whole arm text. Block and flow mappings both
// occur in the corpus (`params: {left: X, right: Y, relation: <=}`), so both
// are parsed.
//
// # Known debt
//
// First-run census (2026-07-29): NONE. All eleven unanchored arms were cured
// with `require_symbol: left` in the change that installed the ratchet, so
// #15103 ABOLISHED the known-debt list. All eleven pre-existing unanchored
// arms were cured with require_symbol: left in the change that installed this
// ratchet, so .formwork/allowlists/count-relation-unanchored-arms.txt shipped
// EMPTY and its only remaining purpose was to be added to. The file and the
// read are gone: every unanchored arm is a finding and there is nowhere to
// record an exception. Historical note — the
// list is SELF-CLEANING: an entry whose arm stops tripping fails this census
// as stale, so curing an arm forces its entry out.
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
	armID       = regexp.MustCompile(`^\s*- id:\s*(\S+)`)
	paramsKey   = regexp.MustCompile(`^(\s*)params:\s*(.*)$`)
	anchorInMap = regexp.MustCompile(`(^\s*|[{,]\s*)(funcs|require_symbol)\s*:`)
)

// stats is the census part of the run, printed on every invocation so the
// ratchet's blast radius is visible in CI logs without a separate query.
type stats struct {
	files     int
	arms      int
	countArms int
}

// paramsHaveAnchor reports whether the arm's own `params:` block declares
// `funcs:` or `require_symbol:`. Only the params block counts: an anchor key
// named in a comment, in the rule's cure prose, or in a sibling arm is not
// this arm's anchor, and a whole-arm text scan would wave those through.
//
// Both YAML mapping styles appear in the corpus. A block mapping puts each key
// on its own more-indented line below `params:`; a flow mapping puts them all
// on the `params:` line inside braces. The line-leading `[{,]` alternative in
// anchorInMap is what reads the flow form, which a `^\s*funcs:` anchor would
// miss entirely — and missing it would be a FALSE FLAG on an arm that is in
// fact anchored.
func paramsHaveAnchor(lines []string, start, end int) bool {
	for i := start; i < end; i++ {
		m := paramsKey.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if anchorInMap.MatchString(m[2]) {
			return true
		}
		keyIndent := len(m[1])
		for j := i + 1; j < end; j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				continue // blank lines cannot end a YAML block mapping
			}
			indent := len(l) - len(strings.TrimLeft(l, " \t"))
			if indent <= keyIndent || armID.MatchString(l) {
				break
			}
			if anchorInMap.MatchString(l) {
				return true
			}
		}
	}
	return false
}

// detect scans root/.formwork/rules/*.yaml and returns every
// go/per-func-count-relation arm whose params declare no anchor, plus corpus
// stats. It reads the corpus ONLY; allowlist reconciliation is main's job, so
// tests exercise detection against bare fixture trees.
//
// Arms are walked as yaml.Node mappings so rematerialised keys (id not first)
// and comment-only "funcs:" still parse the way the engine does (#14091).
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
			typ := mappingScalar(arm, "type")
			if typ != "go/per-func-count-relation" {
				continue
			}
			st.countArms++
			if mappingHasKey(mappingValue(arm, "params"), "funcs") ||
				mappingHasKey(mappingValue(arm, "params"), "require_symbol") {
				continue
			}
			line := mappingKeyLine(arm, "type")
			out = append(out, census.Finding{
				File: rel,
				Line: line,
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

func mappingHasKey(n *yaml.Node, key string) bool {
	return mappingValue(n, key) != nil
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
		fmt.Fprintln(os.Stderr, "usage: formwork-anchor-census <repo-root>")
		os.Exit(2)
	}
	root := os.Args[1]

	flags, st, err := detect(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "census:", err)
		os.Exit(2)
	}
	fmt.Printf("count-relation anchor census: %d rule files, %d arms, %d go/per-func-count-relation arms\n",
		st.files, st.arms, st.countArms)

	// UNCONDITIONAL since #15103. Every flagged arm is a finding; there is no
	// known-debt list to reconcile against, so census.Reconcile's carried-debt
	// and stale-entry bookkeeping has nothing to do here. The sibling
	// formwork-universal-cure-census still carries live rows and still calls
	// Reconcile — that is why this verdict is written out here rather than by
	// changing the shared helper underneath it.
	const why = "declares neither funcs: nor require_symbol: — the relation holds vacuously when both sides count zero, so renaming the constraining symbol retires the invariant silently (#10517)"
	for _, fl := range flags {
		fmt.Printf("NEW %s:%d: arm %q %s\n", fl.File, fl.Line, fl.Arm, why)
	}
	if len(flags) > 0 {
		files := map[string]bool{}
		for _, fl := range flags {
			files[fl.File] = true
		}
		fmt.Printf("count-relation-arm-is-anchored: %d problem(s); %d flagged arm(s) in %d file(s). Anchor each with require_symbol: (or funcs:) — there is no known-debt list to record it in; #15103 abolished it.\n",
			len(flags), len(flags), len(files))
		os.Exit(1)
	}
	fmt.Printf("count-relation-arm-is-anchored: OK — 0 unanchored arm(s); no exemption is possible.\n")
}
