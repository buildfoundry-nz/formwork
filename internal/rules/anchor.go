package rules

import (
	"fmt"
	"sync/atomic"
)

// AnchorProbe is the fail-closed half of a NAME-anchored rule type.
//
// A rule that selects its subject by name — `funcs:` on the Go analyzers,
// `method:` on dart/method-delegates, a `sequence:` stage — skips whatever the
// anchor does not match and then reports nothing. That makes an empty anchor
// set indistinguishable from full compliance: the rule passes its fixture,
// somebody renames the subject for an unrelated reason, and the invariant is
// retired with no signal at all. A gate that is green because it can no longer
// see its own subject is worse than a missing gate, because the dashboard says
// the invariant is held.
//
// The verdict is deliberately SCOPE-WIDE rather than per-file: a rule scoped to
// a package where only one file declares the anchored func is compliant, and
// only a scope in which NO file declares it is a finding. It therefore lands in
// Finalize, and the owning rule must report itself a WholeTreeInvariant — under
// --staged the file bearing the anchor may sit outside the changeset, and a
// per-changeset verdict would read that absence as a rename.
//
// Observe/Hit are called from the engine's per-file worker pool, so both are
// atomic. Safe for concurrent use; the zero value is ready.
type AnchorProbe struct {
	seen  atomic.Bool // a file the anchor could have matched was scanned
	found atomic.Bool // the anchor matched at least once
}

// Observe records that a file the anchor could have matched was scanned. A
// scope that matched zero files leaves the probe unarmed, and that stays
// correct: an unarmed probe means "nothing to anchor against", which is a fact
// about the SCOPE rather than about this anchor, and every rule type would have
// to re-derive it. It is reported once, by identity, one level up — `check`'s
// scan summary discloses such rules on a whole-tree run (naming the first ten
// and counting the rest in its line formats, all of them under -format json),
// and `formwork lint`'s empty-scope fails them (spec §11).
func (a *AnchorProbe) Observe() { a.seen.Store(true) }

// Hit records that the anchor matched.
func (a *AnchorProbe) Hit() { a.found.Store(true) }

// Dead reports whether the scope was non-empty and the anchor matched nothing.
func (a *AnchorProbe) Dead() bool { return a.seen.Load() && !a.found.Load() }

// Verdict returns the missing-anchor finding, or nil when the anchor is live.
// what names the param the anchor came from (e.g. "funcs anchor") so the cure
// points at the field to update.
func (a *AnchorProbe) Verdict(what, pattern string) []Match {
	if !a.Dead() {
		return nil
	}
	return []Match{{Message: fmt.Sprintf(
		"%s %q matched nothing in any in-scope file: the rule can no longer see its subject and now passes vacuously. Update the anchor to the current name, or delete the rule if the invariant is genuinely gone",
		what, pattern)}}
}
