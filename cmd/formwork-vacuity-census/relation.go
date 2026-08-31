// Relation probes. A set-relation or pair-consistency rule is satisfied by an
// EMPTY tree, so none of the witness probes in classify.go can read one: "this
// file satisfies the rule alone" carries no information about where the
// evidence lives. Until #12178 that put both types outside the class-2
// population entirely — isExistenceObligation admitted only
// required-pattern(exists) and pattern-count(at-least) — so nothing had ever
// probed them, and #12180 / #12181 then found live vacuity in exactly those two.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// relationVerdicts runs the probe that is valid for each RELATION rule type.
// Neither is a witness probe: a relation is satisfied by an empty tree, so
// "this file satisfies it alone" carries no information.
//
//   - INERT-REQUIRED-SIDE (set-relation, #12180) — blank every file the
//     required side (params.b.files) draws from and re-run. If the relation
//     still holds, nothing on that side was load-bearing: the join is satisfied
//     by names that appear anyway, and deleting the whole required side is a
//     green change. That is the measured shape of "68/68 codes survive deleting
//     every recompute test".
//   - DEAD-TRIGGER (pair-consistency, #12181) — the `trigger` pattern matches
//     no line of any in-scope file, so no unit ever enters the obligation and
//     the rule is green whatever the tree does. This is the pair-consistency
//     spelling of the engine's own AnchorProbe: a rule that can no longer see
//     its subject is worse than a missing rule, because the enumeration says
//     the invariant is held.
func relationVerdicts(r *config.Rule, root string, m ruleMeta, inScope []*scan.File) []verdict {
	if !satisfiesSet(r, root, inScope) {
		// The rule is failing on the live tree already; the census reports
		// vacuity, not open violations, and a red rule is demonstrably not vacuous.
		return nil
	}
	switch r.Type {
	case "set-relation":
		return setRelationVerdicts(r, root, m, inScope)
	case "pair-consistency":
		return pairConsistencyVerdicts(r, root, m, inScope)
	}
	return nil
}

// setRelationVerdicts asks ONE question, once, and only where the answer means
// something: with every file the required side draws from blanked, does the
// relation still hold? If it does, the required side contributes nothing and no
// tree can falsify the rule.
//
// The two guards below are not narrowing — they are the conditions under which
// blanking params.b is a probe at all. Without them the verdict is a statement
// about the rule's GLOB TOPOLOGY, and the whole measured population of 23 on
// this corpus was exactly that, with no residue:
//
//   - probeableRelation — for `disjoint`, params.b is the FORBIDDEN side, not a
//     required one: A ∩ B = ∅ is satisfied by an empty B BY DEFINITION, so
//     every disjoint rule in every corpus reads as vacuous. Six of the 23 were
//     disjoint rules that fire on their real defect. A disjoint relation is
//     falsified by ADDING a shared element; the census can blank, and blanking
//     can never falsify it. That is a named limit of this instrument, recorded
//     in the report rather than papered over with a verdict.
//   - sidesAreSeparable — when the sides draw from the same files, blanking
//     "the b side" blanks the a side with it, A empties alongside B, and the
//     relation holds trivially. Seven of the 23 were this (five declaring the
//     identical glob twice, two declaring a glob that contains the other side).
//
// The remaining ten came from a binary search that generalised "no single file
// is load-bearing" from testing exactly ONE candidate — the prefix boundary.
// Brute-forcing route-has-test found a load-bearing file in 7 tries, so the
// claim was false, not merely unproven. The search is gone: a verdict this
// instrument cannot decide in one experiment is not a verdict it may report.
// undecidableRelations counts the set-relation rules the blank-the-b-side
// experiment cannot answer, so the report can say so instead of printing a zero
// that reads as coverage.
func undecidableRelations(cfg *config.Config, meta map[string]ruleMeta) int {
	n := 0
	for _, r := range cfg.Rules {
		if relationUndecidedReason(r, meta[r.ID]) != "" {
			n++
		}
	}
	return n
}

// relationUndecidedReason says WHY the census can render no vacuity verdict on
// a set-relation rule, in the author's own vocabulary, or "" when it can render
// one. It is the single source for that question: the count report() prints and
// the set newrules.go refuses both read it, so the number the census admits it
// skipped and the set it refuses to let grow can never drift apart.
//
// The reason is a sentence rather than a code because of what the caller does
// with it. A rule refused with "vacuous" and no actionable reason gets routed
// around — the author cannot tell an unfalsifiable relation from a probe that
// does not apply, and the cures are opposite.
func relationUndecidedReason(r *config.Rule, m ruleMeta) string {
	if r.Type != "set-relation" {
		return ""
	}
	if !probeableRelation(m.relation) {
		return fmt.Sprintf("relation %q is not one the blank-b experiment can falsify. Blanking a side "+
			"cannot break a disjointness, and params.b is the REQUIRED side only on subset/equal, so no "+
			"probe here decides it", m.relation)
	}
	if !sidesAreSeparable(m) {
		return fmt.Sprintf("the two sides are drawn from OVERLAPPING globs (a: %v, b: %v), so blanking "+
			"params.b blanks params.a with it: the relation then holds for a reason that is about the "+
			"scope declaration and not about the tree", m.aFiles, m.bFiles)
	}
	return ""
}

func setRelationVerdicts(r *config.Rule, root string, m ruleMeta, inScope []*scan.File) []verdict {
	var out []verdict
	// EMPTY-SIDE (V4 / #12429): on equal|subset, either side extracting zero
	// elements from the live tree means the join is green by set algebra
	// (empty=empty / empty⊆B) with no load-bearing evidence. Gate it before
	// the blank-B probe so a zero-cardinality relation is never OK'd.
	if probeableRelation(m.relation) {
		if v := emptySideVerdict(r, m, inScope); v != nil {
			out = append(out, *v)
		}
	}
	if len(m.bFiles) == 0 || !probeableRelation(m.relation) || !sidesAreSeparable(m) {
		return out
	}
	side, err := config.New("perglob", "forbidden-pattern", finding.SeverityError, "", m.bFiles, nil, nil, nil)
	if err != nil {
		return out
	}
	off := map[string]bool{}
	for _, f := range inScope {
		if side.Applies(f.Path()) {
			off[f.Path()] = true
		}
	}
	if len(off) == 0 {
		return out
	}
	if !satisfiesSet(r, root, blankPaths(inScope, off)) {
		return out // the required side is load-bearing: emptying it trips the rule
	}
	return append(out, verdict{
		class: class2, code: "INERT-REQUIRED-SIDE", gating: true,
		why: fmt.Sprintf("blanking all %d file(s) the required side (params.b.files) draws from leaves the "+
			"relation satisfied, so the whole side could be deleted and this rule would stay green. The join "+
			"is carried by names that appear anyway; no tree can falsify it", len(off)),
		evidence: []string{"required-side globs: " + strings.Join(m.bFiles, ", ")},
	})
}

// emptySideVerdict reports a live equal|subset rule whose either extracted set
// has cardinality 0 on the real tree. That is the set-algebra free pass the
// engine's min_count floor closes when authors set it; the census gates the
// residual so undeclared zero-cardinality joins still surface.
func emptySideVerdict(r *config.Rule, m ruleMeta, inScope []*scan.File) *verdict {
	if m.aPattern == "" || m.bPattern == "" {
		return nil
	}
	aN, err := sideCardinality(r, inScope, m.aFiles, m.aPattern, m.aGroup, m.aPreprocess)
	if err != nil {
		return nil
	}
	bN, err := sideCardinality(r, inScope, m.bFiles, m.bPattern, m.bGroup, m.bPreprocess)
	if err != nil {
		return nil
	}
	if aN > 0 && bN > 0 {
		return nil
	}
	return &verdict{
		class: class2, code: "EMPTY-SIDE", gating: true,
		why: fmt.Sprintf("equal/subset side cardinalities on the live tree are A=%d B=%d; a zero-cardinality "+
			"side makes the relation hold by set algebra with no load-bearing evidence (empty=empty / empty⊆B). "+
			"Repoint the side pattern, or set params.a/b.min_count ≥ 1 so the engine refuses the empty join",
			aN, bN),
		evidence: []string{
			fmt.Sprintf("a.files=%s a.pattern=%q → %d", strings.Join(m.aFiles, ","), m.aPattern, aN),
			fmt.Sprintf("b.files=%s b.pattern=%q → %d", strings.Join(m.bFiles, ","), m.bPattern, bN),
		},
	}
}

// sideCardinality re-extracts one set-relation side the way the engine does:
// glob match → optional per-side preprocess stacked on the rule preprocess →
// capture-group values. Count only, never the set contents.
func sideCardinality(r *config.Rule, inScope []*scan.File, globs []string, pattern string, group int, sidePreprocess string) (int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, err
	}
	if group < 0 || group > re.NumSubexp() {
		return 0, fmt.Errorf("group %d out of range", group)
	}
	values := map[string]bool{}
	for _, f := range inScope {
		if !globListMatches(globs, f.Path()) {
			continue
		}
		work, err := f.Variant(r.Preprocess)
		if err != nil {
			continue
		}
		if sidePreprocess != "" {
			work, err = work.Variant(sidePreprocess)
			if err != nil {
				continue
			}
		}
		lines, err := work.Lines()
		if err != nil {
			continue
		}
		for _, line := range lines {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				if group < len(m) {
					values[m[group]] = true
				}
			}
		}
	}
	return len(values), nil
}

func globListMatches(globs []string, path string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, path); ok {
			return true
		}
	}
	return false
}

// probeableRelation reports whether params.b is the side the relation REQUIRES,
// which is what makes emptying it a question. `subset` (A ⊆ B) and `equal`
// (A = B) are falsified by B losing an element. `disjoint` (A ∩ B = ∅) is not:
// it is falsified by GAINING one, and is satisfied by an empty B by definition.
func probeableRelation(rel string) bool { return rel == "subset" || rel == "equal" }

// sidesAreSeparable reports whether blanking the b side leaves the a side
// standing. It does not, when either glob list can match the other's files —
// then A empties with B and the relation holds for a reason that is about the
// scope declaration, not about the tree. Matching is by glob, both directions,
// so `a: [x/**]` with `b: [x/one.go]` is caught as surely as two identical
// globs are.
func sidesAreSeparable(m ruleMeta) bool {
	if len(m.aFiles) == 0 {
		return false
	}
	for _, a := range m.aFiles {
		for _, b := range m.bFiles {
			if a == b || globCovers(a, b) || globCovers(b, a) {
				return false
			}
		}
	}
	return true
}

// globCovers reports whether pattern could match anything outer matches. A
// literal path is tested directly; a pattern is tested by its own literal
// prefix, which is the part any file it matches must start with.
func globCovers(outer, inner string) bool {
	if ok, err := doublestar.Match(outer, inner); err == nil && ok {
		return true
	}
	prefix := inner
	if i := strings.IndexAny(inner, "*?["); i >= 0 {
		prefix = inner[:i]
	}
	if prefix == "" || prefix == inner {
		return false
	}
	ok, err := doublestar.Match(outer, strings.TrimSuffix(prefix, "/"))
	return err == nil && ok
}

// blankPaths returns fs with the named paths replaced by empty in-memory twins,
// so a probe removes content without removing the file from the corpus.
func blankPaths(fs []*scan.File, off map[string]bool) []*scan.File {
	out := make([]*scan.File, 0, len(fs))
	for _, f := range fs {
		if off[f.Path()] {
			out = append(out, scan.NewMemFile(f.Path(), nil))
			continue
		}
		out = append(out, f)
	}
	return out
}

// pairConsistencyVerdicts reports a pair-consistency rule whose obligation
// entry condition matches nothing in scope. No unit ever enters the obligation,
// so the `requires` half is never asked for and the rule is green over any tree.
//
// The entry condition is trigger (always) AND also_present when set (I17): an
// also_present miss is the same as no trigger — checkFuncSpan returns without
// asking for requires. Matching only the trigger would call a rule "live" while
// every unit fails also_present and the companion is never enforced.
//
// Unit semantics follow `where` (I18): same-func parses FuncDecl / var-bound
// FuncLit spans and MatchString on the full span (multi-line triggers work;
// package-level residue does not count). same-file / same-dir / default match
// per line (or whole-file for also_present co-presence).
//
// The probe deliberately is NOT "duplicate a trigger and see whether the rule
// notices the extra". `where: same-file` is this type's DEFAULT and its
// documented meaning — "a trigger match obliges a requires match within the
// same UNIT" (internal/rules/pairconsistency doc comment) — so a second trigger
// beside an existing companion is compliant BY DEFINITION and a green there is
// the rule working. Measured against this corpus that probe flagged 93 of 106
// pair-consistency rules: it was reading the rule type, not the rule. Only 3 of
// the 106 declare where: same-func and 10 where: same-dir; the rest take the
// default, which is exactly the population such a probe would condemn.
func pairConsistencyVerdicts(r *config.Rule, root string, m ruleMeta, inScope []*scan.File) []verdict {
	if m.trigger == "" {
		return nil
	}
	// MUST use the same engine as pair-consistency itself (Go regexp, not
	// regexp2). POSIX classes like [[:space:]] and [:alnum:] are load-bearing
	// in corpus triggers; regexp2 treats them as literal character sets and
	// falsely reports DEAD-TRIGGER on rules the formwork gate still enforces
	// (measured: go-compile-gate-disk-probe is live under Go regexp, "dead"
	// under regexp2 — #12178 / #12181).
	re, err := regexp.Compile(m.trigger)
	if err != nil {
		return nil
	}
	var alsoRe *regexp.Regexp
	if m.alsoPresent != "" {
		alsoRe, err = regexp.Compile(m.alsoPresent)
		if err != nil {
			return nil
		}
	}
	where := m.where
	if where == "" {
		where = "same-file"
	}
	for _, f := range inScope {
		// The trigger is matched against the file the ENGINE sees, not the raw
		// bytes: a rule declaring decomment-go never sees a trigger that lives
		// only in a comment, and reading the raw file would call that live.
		work, err := f.Variant(r.Preprocess)
		if err != nil {
			continue
		}
		if where == "same-func" {
			if strings.HasSuffix(f.Path(), ".go") {
				if obligationLiveSameFunc(work, re, alsoRe) {
					return nil
				}
				continue
			}
			// Dart/proto same-func units live in the engine (#12195). This
			// census cannot parse those languages, so a trigger that appears
			// at all is live — not DEAD-TRIGGER for want of a Go parser.
			if obligationLivePerLine(work, re, alsoRe) {
				return nil
			}
			continue
		}
		if obligationLivePerLine(work, re, alsoRe) {
			return nil
		}
	}
	why := fmt.Sprintf("the trigger %q matches no obligation-entering unit of any of the %d in-scope file(s) "+
		"(where=%s), so the `requires` half is never asked for. Repoint the trigger at its current spelling, "+
		"or delete the rule if the subject is genuinely gone", m.trigger, len(inScope), where)
	if alsoRe != nil {
		why = fmt.Sprintf("no in-scope unit matches trigger %q ∧ also_present %q (where=%s) across %d file(s), so no unit "+
			"enters the obligation and the `requires` half is never asked for. Repoint also_present / trigger, or "+
			"delete the rule if the subject is genuinely gone", m.trigger, m.alsoPresent, where, len(inScope))
	}
	return []verdict{{
		class: class2, code: "DEAD-TRIGGER", gating: true,
		why: why,
	}}
}

// obligationLivePerLine is the same-file / same-dir DEAD-TRIGGER unit: a line
// matching the trigger, and when also_present is set the whole file must also
// match it (also_present is currently engine-valid only for same-func, but the
// census stays defensive if a future rule loosens that).
func obligationLivePerLine(f *scan.File, trigger, also *regexp.Regexp) bool {
	lines, err := f.Lines()
	if err != nil {
		return false
	}
	trig := false
	for _, l := range lines {
		if trigger.MatchString(l) {
			trig = true
			break
		}
	}
	if !trig {
		return false
	}
	if also == nil {
		return true
	}
	content, err := f.Content()
	if err != nil {
		return false
	}
	return also.Match(content)
}

// obligationLiveSameFunc mirrors pair-consistency checkSameFunc unit geometry:
// top-level FuncDecls (with body) and outermost var-bound FuncLits. A multi-line
// trigger that only matches across lines inside a span is LIVE here and DEAD
// under per-line matching — the I18 residual this closes.
func obligationLiveSameFunc(f *scan.File, trigger, also *regexp.Regexp) bool {
	if !strings.HasSuffix(f.Path(), ".go") {
		return false
	}
	content, err := f.Content()
	if err != nil {
		return false
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f.Path(), content, parser.AllErrors)
	if err != nil {
		return false
	}
	live := false
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			if spanMatchesObligation(content, fset, d, trigger, also) {
				live = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, value := range vs.Values {
					for _, fl := range outermostFuncLits(value) {
						if spanMatchesObligation(content, fset, fl, trigger, also) {
							live = true
						}
					}
				}
			}
		}
	}
	return live
}

func spanMatchesObligation(content []byte, fset *token.FileSet, node ast.Node, trigger, also *regexp.Regexp) bool {
	start := fset.Position(node.Pos()).Offset
	end := fset.Position(node.End()).Offset
	if start < 0 || end > len(content) || start >= end {
		return false
	}
	span := string(content[start:end])
	if !trigger.MatchString(span) {
		return false
	}
	if also != nil && !also.MatchString(span) {
		return false
	}
	return true
}

// outermostFuncLits mirrors pairconsistency.outermostFuncLits: func literals
// not nested inside another func literal.
func outermostFuncLits(e ast.Expr) []*ast.FuncLit {
	var out []*ast.FuncLit
	ast.Inspect(e, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			out = append(out, fl)
			return false
		}
		return true
	})
	return out
}
