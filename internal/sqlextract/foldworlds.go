package sqlextract

import (
	"sort"
	"strings"
)

// WORLD ENUMERATION — the second half of the assignment-flow fold (#36, #42).
//
// fold.go walks the statements and decides WHICH APPENDS a tracked variable
// has and what guards each one; this file decides WHICH VALUES that variable is
// therefore worth reporting. The seam is `foldWorlds`, the only entry point
// fold.go uses, and the split is by question rather than by size: nothing here
// reads an ast.Node, and nothing in fold.go decides a world.
//
// The contract block at the top of fold.go governs both halves and is the
// document `unseenwrite.go` defers to. EMISSION ONLY EVER GROWS is this file's
// invariant in particular — every removal reasoned about here was measured and
// rejected — so read foldWorlds' own comment before changing what it returns.
// foldWorlds enumerates the candidate texts for one tracked variable.
//
// The base model is the spec's bounded all-or-nothing pair: `full` (every
// optional append) and `base` (none). Both are ALWAYS emitted. When two optional
// appends are guarded by a condition and its negation (`if a` / `if !a`) the
// two branch worlds are emitted as well — that pair is reachable one branch at a
// time, and `full`/`base` model neither of them, so a lock in one branch with
// the order in the other went unreported before (spec §9, miss 1).
//
// EMISSION ONLY EVER GROWS. Adding worlds cannot silence anything; removing them
// can, and in a lockdown gate silence on a deadlock hazard is the failure nobody
// sees. Four review rounds of this file were spent trying to remove `base` under
// a complementary pair — #42's false positive, since `a` and `!a` cannot both be
// false — and each attempt removed a reachable world instead, on ordinary Go: a
// helper call handed an options struct, a method value, a pointer in a composite
// literal, an embedded field, a closure. Removing `base` needs proof that the
// two `if`s read ONE value, which needs alias analysis, which needs types this
// pass deliberately does not have.
//
// And even a correct proof is not enough. Complementarity says the FINAL value
// never lacks both appends; it says nothing about the value in between, so a
// query observed between the two branches (`run(q)`, an early return, a
// db.Query) is `base` on a real path. No analysis of writes could have found
// that, because there is no write.
//
// So #42's false positive stays, disclosed (spec §9, locking.go): `base` may be
// a world no path produces. It is a finding a `formwork:allow` marker clears,
// which is the trade a lockdown gate takes over silence.
func foldWorlds(v *foldVar) []string {
	worlds := []string{foldText(v, nil, true), foldText(v, nil, false)}
	// EACH PAIR SEPARATELY, never multiplied. A world fixes exactly ONE truth
	// value; every other pair's appends stay segOpen and keep the all-or-nothing
	// bound. So no world asserts a combination of flags, and the cross-product
	// that would invent one — the reachable-only-if-independent problem `b := a`
	// refutes, which this pass cannot check — is never formed.
	//
	// Enumerating only the FIRST pair was the previous rule, and it was the
	// silent direction: a variable with two complementary flags has more hazard
	// surface than one with a single flag and was given less analysis, because
	// the second pair dropped it to full/base alone
	// (TestFromGoReassembledEachPairEmitsItsOwnBranchWorlds).
	cands := complementaryCandidates(v.segs)
	if len(cands) > maxEnumeratedPairs {
		// Cost, not correctness: every text here is a pg_query parse on wazero,
		// and those dominate the rule's runtime. Truncating still leaves full,
		// base, and the earlier pairs' worlds — strictly more than the one-pair
		// rule emitted — and an append belonging to a dropped pair is still
		// covered by the all-or-nothing bound. Candidates are sorted, so which
		// pairs survive is deterministic. Disclosed in locking.go and spec §9.
		cands = cands[:maxEnumeratedPairs]
	}
	for _, path := range cands {
		worlds = append(worlds, branchWorlds(v, path)...)
	}
	return worlds
}

// maxEnumeratedPairs bounds branch-world emission at 2 + 4×N texts per tracked
// variable. Three covers the query builders this targets; beyond it the parse
// cost stops being worth the reach.
const maxEnumeratedPairs = 3

// branchWorlds renders both branches of one complementary name, each minimally
// and — when the assignment leaves some append undetermined, the only way the
// two differ — maximally.
func branchWorlds(v *foldVar, path string) []string {
	out := make([]string, 0, 4)
	for _, branch := range []bool{false, true} {
		chosen := map[string]bool{path: branch}
		out = append(out, foldText(v, chosen, false))
		if anyOpen(v.segs, chosen) {
			out = append(out, foldText(v, chosen, true))
		}
	}
	return out
}

// anyOpen reports whether chosen leaves some optional append undetermined.
func anyOpen(segs []foldSeg, chosen map[string]bool) bool {
	for _, seg := range segs {
		if seg.optional && segIn(seg, chosen) == segOpen {
			return true
		}
	}
	return false
}

// complementaryCandidates returns the paths that appear as the LAST guard of two
// optional appends — once plainly, once negated — under an IDENTICAL enclosing
// prefix.
//
// Prefix equality is what makes the claim true. An append fires on the
// conjunction of every guard enclosing it, so two appends are complementary
// exactly when they agree on all but the last guard and disagree on that one:
// `useTx && a` against `useTx && !a` is as much a pair as `a` against `!a`, and
// at least one of the two fires whenever the shared prefix holds.
//
// This deliberately does NOT pair across differing prefixes. An append under
// `if a { if b { … } }` (prefix `a`) says nothing about an `if !b` elsewhere
// (prefix empty) — a=false skips both, so neither is forced by fixing b.
// TestFromGoReassembledPairEligibilityBoundary pins that exclusion; keying on
// the innermost guard alone would invent the pair.
//
// A sole-guard rule was the first cut at this and lost the nested pair
// entirely: two appends under one `if useTx` were modelled by `full` alone, so
// a lock in one branch with the order in the other went unreported — silent, in
// the shape a transaction guard makes ordinary
// (TestFromGoReassembledPairUnderSharedGuardEmitsItsBranchWorlds).
//
// The seg.opaque skip here is PRECISION, not safety: an opaque append is not
// determined by its guards, so pairing on one buys worlds that segIn will only
// ever leave open. Dropping it emits redundant candidates (parse cost), never
// deletes one — no test covers it and none needs to. The load-bearing use of
// seg.opaque is in segIn, where it stops an undetermined append being rendered
// as forced; that one has a hazard riding on it and is pinned by
// TestFromGoReassembledOpaqueNestedConditionIsNotForcedByItsGuard.
func complementaryCandidates(segs []foldSeg) []string {
	type pairKey struct{ prefix, path string }
	plain, negated := map[pairKey]bool{}, map[pairKey]bool{}
	for _, seg := range segs {
		if !seg.optional || seg.opaque || len(seg.guards) == 0 {
			continue
		}
		last := seg.guards[len(seg.guards)-1]
		k := pairKey{prefix: guardPrefixSig(seg.guards[:len(seg.guards)-1]), path: last.path}
		if last.negated {
			negated[k] = true
		} else {
			plain[k] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for k := range plain {
		// One entry per path: two prefixes pairing on the same name still fix
		// one truth value, so branchWorlds has nothing extra to enumerate.
		if negated[k] && !seen[k.path] {
			seen[k.path] = true
			out = append(out, k.path)
		}
	}
	sort.Strings(out)
	return out
}

// guardPrefixSig renders a guard conjunction as a comparable string. `!` cannot
// appear in a path (guardPath admits only idents and field chains) and neither
// can a newline, so the encoding is unambiguous.
func guardPrefixSig(guards []guardRef) string {
	var b strings.Builder
	for _, g := range guards {
		if g.negated {
			b.WriteByte('!')
		}
		b.WriteString(g.path)
		b.WriteByte('\n')
	}
	return b.String()
}

// segState is how a world's truth assignment settles one optional append.
type segState int

const (
	// segOpen: the assignment leaves it free — no guard of it is fixed, some
	// enclosing condition is opaque, or only part of its conjunction is fixed.
	segOpen segState = iota
	// segHolds: every condition it needs is fixed in its favour.
	segHolds
	// segFails: some condition it needs is fixed against it.
	segFails
)

// segIn reports how chosen settles seg.
func segIn(seg foldSeg, chosen map[string]bool) segState {
	fixed := 0
	for _, g := range seg.guards {
		holds, decided := chosen[g.path]
		if !decided {
			continue
		}
		if holds == g.negated {
			return segFails
		}
		fixed++
	}
	if !seg.opaque && fixed > 0 && fixed == len(seg.guards) {
		return segHolds
	}
	return segOpen
}

// foldText renders one world. Unconditional appends always apply. An optional
// append applies when this world's truth assignment forces it, never when the
// assignment forces one of its enclosing conditions false, and otherwise iff
// indep — preserving the all-or-nothing bound over appends the assignment does
// not reach. chosen == nil with indep true/false therefore reproduces
// `full`/`base` exactly.
func foldText(v *foldVar, chosen map[string]bool, indep bool) string {
	text := v.seed
	for _, seg := range v.segs {
		if !seg.optional {
			text += seg.text
			continue
		}
		switch segIn(seg, chosen) {
		case segFails:
			continue
		case segHolds:
			text += seg.text
		default:
			if indep {
				text += seg.text
			}
		}
	}
	return text
}
