package cli

import (
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Split out of cli.go for the same reason scope.go was: not because cli.go had
// crossed the 750-line cap, but because this change would have taken it over.
// This
// half is the seam runScope and runCheck share, so it belongs to neither file.
// It is not a pure move: the six duplicated lines became one function that takes
// a statuses parameter, and its branch is written the other way round — the old
// runCheck asked `if staged` and this asks `if !staged && rangeSpec != ""`, so
// the no-flag default lands on staged from the else arm rather than the if arm.

// changesetStatuses selects which git statuses count as a change, because the
// two callers of changesetFor are asking different questions of the same flags.
type changesetStatuses bool

const (
	// scannableOnly: added/copied/modified/renamed — the statuses whose path
	// still exists in the INDEX, i.e. everything a scan could be asked to open.
	// What check needs; it opens every path it is given. NOT a promise about the
	// working tree: a staged path deleted from disk afterwards is still ACMR,
	// which is what refuseUnaccountedPaths (#158) exists for.
	scannableOnly changesetStatuses = false
	// anyStatus: every path git reports as differing, deletions included, and
	// both ends of a rename. What scope needs; it classifies a change rather
	// than reading one, and deleting or renaming away a source file is a change
	// (#147).
	anyStatus changesetStatuses = true
)

// changesetFor turns the (--staged, --range) flag pair into the set of changed
// paths, for whichever command is asking.
//
// This seam did not exist: the same branch was written out twice, in runScope
// and runCheck, and the two commands drifted apart on exactly this flag pair
// before — see rangeValueUsable's comment, where a guard added to runCheck alone
// left runScope answering the opposite thing. One function is what stops a third
// spelling appearing.
//
// POLICY STAYS WITH THE CALLER. This returns what git said; it does not decide
// what an empty answer means. runScope treats empty as unusable and assumes
// runtime; runCheck keeps its requested-vs-scanned disclosure and its exit 0,
// which is the right answer for a command that gates on findings rather than on
// how many files it was handed. Pushing that judgement down here would give both
// commands one policy neither of them asked for.
//
// staged wins if both are somehow set, which is runCheck's existing precedence.
// Both callers refuse that combination before reaching here — runScope with an
// explicit mutual-exclusion check, runCheck with the same one — so the tie-break
// is written down rather than relied upon.
//
// Not every path into a changeset comes through here, and the ones that do not
// are named rather than assumed: gitdiff rules take their range from config and
// resolve it inside the engine's phase-2 pool, reaching no CLI seam at all, and
// a stray positional argument makes flag.Parse discard a later --range before
// any of this runs. Both are recorded in docs/plans/2026-08-14-scope-empty-changeset.md.
func changesetFor(root string, staged bool, rangeSpec string, statuses changesetStatuses) ([]string, error) {
	if !staged && rangeSpec != "" {
		if statuses == anyStatus {
			return vcs.RangePathsAnyStatus(root, rangeSpec)
		}
		return vcs.RangePaths(root, rangeSpec)
	}
	if statuses == anyStatus {
		return vcs.StagedPathsAnyStatus(root)
	}
	return vcs.StagedPaths(root)
}
