package main

// newrules.go — the arm that refuses a rule this change ADDS which the census
// cannot decide (#15837).
//
// Every other arm here answers "is this rule vacuous?". This one answers the
// question one level up: "was that question even asked?". The census gates what
// it DECIDES, and it declines to decide two whole populations, which it prints
// out loud in report() precisely because a hole that goes unnamed reads as
// coverage:
//
//   - a set-relation the blank-b experiment cannot falsify — `disjoint`, or two
//     sides drawn from OVERLAPPING globs (relationUndecidedReason);
//   - a name-anchored go rule whose anchor is package-qualified, which the
//     census has no type information to resolve (symbolAnchorUndecidedReason).
//
// Printing a count beside "OK: every rule can fail" does not stop a new rule
// from landing inside the hole, and a declined verdict is otherwise
// indistinguishable from a pass: exit 0, lane green, nobody ever asked whether
// the rule can fire. That is this campaign's own pattern — an instrument
// reporting green about something it never evaluated — sitting inside the tool
// we use to measure that pattern.
//
// It is not theoretical. A set-relation whose baseline file sits inside its own
// B-side was found BY HAND twice during this campaign
// (partition-parents-have-partman-parent, where a: schema.snapshot.sql is
// itself matched by b: migrations/**/*.sql; and proto-internal-live-messages,
// where adding a baseline row is what MAKES a dead message live). Both are the
// overlapping-globs case. Two hand-found instances of a class the instrument
// silently skips is the argument for not skipping it on new rules.
//
// # Why ADDED rules only
//
// Measured on the corpus this landed against: 19 of 90 set-relation rules and
// 38 of 49 symbol-anchored rules, 57 of 2317 in all. Gating the whole corpus
// would open 57 findings and block every PR until they are closed, and a gate
// that cannot be turned on is worth nothing. Those 57 are the #15837 backfill.
// This is the same new-rules-first rollout rule-has-mutation-proof used for the
// identical reason (#11934), and the backfill is the later wave, not something
// this arm silently skips.
//
// # Why it lives INSIDE the census
//
// A second vacuity instrument that disagreed with this one about what "cannot
// fire" means would be worse than the hole it closed, and the census is the
// stated authority — vacuity is never re-derived from rule YAML. There is also
// a wall-clock reason: the census takes over ten minutes standalone on this
// tree, and a new type:command rule re-running vacuity analysis would put a
// second one of those on the Architecture lane's critical path (#14875). This
// arm reads rule METADATA the census has already loaded and shells out to git
// once, so it costs a single `git diff` restricted to .formwork/rules.
//
// # Why the reasons are sentences
//
// The verdict carries the population's own explanation and the edit that cures
// it, not a code. An author refused with "vacuous" and nothing else cannot tell
// an unfalsifiable relation from a probe that does not apply — and the cures
// are opposite — so they route around the gate instead.
//
// # The base case
//
// A root that is not a git checkout is a mutation-proof scratch or a synthetic
// corpus: there is no change to judge and the arm is inert. That is stated, not
// an escape hatch — it is unreachable from CI and from a developer checkout,
// both of which are git repos, and in a git checkout an unresolvable range is
// exit 2, never an empty added set. An empty added set would be a silent pass
// for every rule the change introduces. Same contract, and the same range
// precedence, as scripts/dev/mutation-proof.
//
// # scan.Walk and .formwork
//
// The engine's scanner drops .formwork/ before rules run, so a rule whose
// subject lives there can never put that subject in scope.include: "the target
// is in scope" and "the scope is scannable" are jointly unsatisfiable, and such
// a rule is unprovable for a structural reason rather than a careless one. This
// arm never reaches that set — it judges rule METADATA (params.a/params.b file
// globs, params.symbol, params.sink), never scope membership, so a subject
// under .formwork/ is not something it can charge anyone for. No exemption is
// needed, and on the corpus this landed against not one of the 57 undecided
// rules has a subject there. An allowlist of rule ids would be the wrong shape
// in any case: this campaign exists to delete those.

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

// ruleIDDecl matches a rule's `- id: <id>` declaration line as it appears in a
// rule file, with the diff's leading +/- already stripped.
var ruleIDDecl = regexp.MustCompile(`^\s*-\s+id:\s*(\S+)\s*$`)

// rulesDirRel is the only path the added-rule diff reads. Restricting the diff
// keeps this arm's git cost to one narrow command however large the range is.
const rulesDirRel = ".formwork/rules"

// addedRuleIDs returns the ids of rules whose declaration this change ADDS.
//
// The second result is false when root is not a git checkout — a mutation-proof
// scratch or a synthetic corpus — where there is no change to judge. In a git
// checkout an unresolvable range is an error, never an empty set: an empty set
// there is a silent pass for every rule the change introduces.
//
// An id that appears on BOTH sides of the diff is a MOVE, not an addition.
// Reading the added lines alone would charge an author for relocating a rule
// between files, which is the one edit that changes nothing about whether it
// can fire.
func addedRuleIDs(root string) (map[string]bool, bool, error) {
	if !isGitCheckout(root) {
		return nil, false, nil
	}
	rangeExpr, err := censusDiffRange(root)
	if err != nil {
		return nil, true, err
	}
	out, err := gitCmd("-C", root, "diff", "--unified=0",
		mergeBaseDiffRange(rangeExpr), "--", rulesDirRel).CombinedOutput()
	if err != nil {
		return nil, true, fmt.Errorf("git diff %s -- %s: %v\n%s", rangeExpr, rulesDirRel, err, out)
	}
	plus, minus := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			if m := ruleIDDecl.FindStringSubmatch(line[1:]); m != nil {
				plus[m[1]] = true
			}
		case strings.HasPrefix(line, "-"):
			if m := ruleIDDecl.FindStringSubmatch(line[1:]); m != nil {
				minus[m[1]] = true
			}
		}
	}
	added := map[string]bool{}
	for id := range plus {
		if !minus[id] {
			added[id] = true
		}
	}
	return added, true, nil
}

func isGitCheckout(root string) bool {
	return gitCmd("-C", root, "rev-parse", "--git-dir").Run() == nil
}

// censusDiffRange applies the same precedence scripts/dev/mutation-proof uses:
// CI's authoritative PR range env (TDD_TWO_COMMIT_SPLIT_RANGE, the #5938 range
// ci.yml exports), then merge-base(origin/develop, HEAD)..HEAD — the checkout's
// own commits. One precedence for both gates so a rule cannot be new to one and
// old to the other.
func censusDiffRange(root string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("TDD_TWO_COMMIT_SPLIT_RANGE")); v != "" {
		return v, nil
	}
	out, err := gitCmd("-C", root, "merge-base", "origin/develop", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("no diff range resolvable in a git checkout — refusing to guess which "+
			"rules are new (fail-closed). Set TDD_TWO_COMMIT_SPLIT_RANGE, or fetch origin/develop so "+
			"merge-base can resolve: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out)) + "..HEAD", nil
}

// mergeBaseDiffRange rewrites a two-dot range A..B to its three-dot form A...B
// so the diff is taken from merge-base(A, B) — the branch's OWN commits —
// instead of comparing the two endpoints. A two-dot `git diff` on a branch whose
// base has fallen behind develop reads every develop-side change since the fork
// point IN REVERSE, so develop's new rules would surface here as this branch's
// additions and the author would be charged for them (#12104, the identical
// normalisation scripts/dev/mutation-proof applies for the identical reason).
// Explicit three-dot ranges and single revs pass through unchanged.
func mergeBaseDiffRange(rangeExpr string) string {
	if strings.Contains(rangeExpr, "...") {
		return rangeExpr
	}
	if i := strings.Index(rangeExpr, ".."); i >= 0 {
		return rangeExpr[:i] + "..." + rangeExpr[i+2:]
	}
	return rangeExpr
}

// newRuleVerdicts gates every rule this change ADDED or EDITED INTO the
// undecidable set. The decidability question is asked through the same two
// functions report() counts through, never re-derived here.
//
// baseReasons is the same question answered at the diff's base. A rule missing
// from it is one this change adds; a rule present with an empty reason was
// DECIDABLE and has been edited into the hole; a rule present with a reason was
// already there and is not this author's doing. Only the first two are refused,
// and they get different sentences because only one of them is an addition.
func newRuleVerdicts(cfg *config.Config, meta map[string]ruleMeta, declared map[string]bool, added map[string]bool, baseReasons map[string]birthFinding) map[string][]verdict {
	out := map[string][]verdict{}
	for _, r := range cfg.Rules {
		f := birthReason(r, meta[r.ID], declared[r.ID])
		if f.reason == "" {
			continue
		}
		reason, cure := f.reason, f.cure
		prev, existedAtBase := baseReasons[r.ID]
		was := prev.reason
		switch {
		case !existedAtBase || added[r.ID]:
			out[r.ID] = append(out[r.ID], verdict{
				class: classNew, code: f.code, gating: true,
				why: f.addedWhy + reason, evidence: []string{cure},
			})
		case was == "":
			// The recall hole an added-id set cannot see: the id is on both sides
			// of the diff, so nothing keyed on addition reports it, and the rule
			// enters the class without ever being born.
			out[r.ID] = append(out[r.ID], verdict{
				class: classNew, code: f.code, gating: true,
				why: f.editedWhy + reason, evidence: []string{cure},
			})
		}
	}
	return out
}

// birthFinding is one population's answer for one rule: the verdict code, the
// reason in the population's own vocabulary, the cure, and the two sentences
// that distinguish a rule this change ADDED from one it EDITED into the shape.
//
// The two sentences live here rather than at the call site because they are the
// only observable difference between the branches, and a population that shared
// them would make its own branches indistinguishable — which is how the addition
// branch shipped untested the first time.
type birthFinding struct {
	code      string
	reason    string
	cure      string
	addedWhy  string
	editedWhy string
}

// birthReason is the ONE question every arm on this detector asks: is there a
// reason this rule may not be born? Both populations answer through it, so base
// and head can never be compared through different code.
//
// Order is deliberate. The undecidable populations come first because they are a
// statement about what the census CANNOT do, and a rule that is undecidable and
// also a wide exists arm is better reported as undecidable — the author cannot
// act on the second until the first is cured.
func birthReason(r *config.Rule, m ruleMeta, declared bool) birthFinding {
	if reason := relationUndecidedReason(r, m); reason != "" {
		return undecidedFinding(reason, relationCure)
	}
	if reason := symbolAnchorUndecidedReason(r); reason != "" {
		return undecidedFinding(reason, symbolAnchorCure)
	}
	if r.Type == "required-pattern" {
		if reason := existsBirthReason(m.mode, r.Include(), declared); reason != "" {
			return birthFinding{
				code: "NEW-EXISTS-ARM-UNDECLARED", reason: reason, cure: existsBirthCure,
				addedWhy: "this change ADDS this arm and it can be satisfied by a file nobody meant, " +
					"with no declared reason: ",
				editedWhy: "this change EDITS this arm INTO a shape it did not have at the base of this " +
					"change, where it can be satisfied by a file nobody meant, with no declared reason: ",
			}
		}
	}
	return birthFinding{}
}

func undecidedFinding(reason, cure string) birthFinding {
	return birthFinding{
		code: "NEW-RULE-UNDECIDED", reason: reason, cure: cure,
		addedWhy: "this change ADDS this rule and the census can render NO vacuity verdict on it, so " +
			"nothing has asked whether it can fire: ",
		editedWhy: "this change EDITS this rule OUT of the set the census can decide — it was decidable " +
			"at the base of this change and is not any more, so nothing now asks whether it can fire: ",
	}
}

const relationCure = "cure: pull the two sides apart so blanking params.b leaves params.a standing — " +
	"point b at the file that actually carries the baseline and keep it out of a's globs (a scope.exclude " +
	"on the b-side path is usually the whole edit), or narrow a. If the sides genuinely cannot be " +
	"separated, the relation is not what this rule means: an A that empties with B holds by set algebra, " +
	"and params.a/b.min_count is how the engine is told to refuse that. Do not cure it by widening b."

const symbolAnchorCure = "cure: anchor on the BARE declared name the census can resolve against the " +
	"tree — `(^|\\.)Name$` rather than `pkg\\.Name` — and let scope.include carry the package. A " +
	"package-qualified anchor cannot be told apart from a symbol that no longer exists, which is the " +
	"state this rule would ship in."

// newRuleIDs renders the added set for the report's own summary line, sorted so
// the output is stable across runs.
func newRuleIDs(added map[string]bool) []string {
	out := make([]string, 0, len(added))
	for id := range added {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
