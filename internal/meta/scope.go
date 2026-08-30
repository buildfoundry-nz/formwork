// scope.go — the shared "what did this rule have to look at" predicate, and
// the prune-channel counts. Split from lint.go, which the 750-line vendor cap
// bounds; same package.
package meta

import (
	"fmt"
	"sort"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/report"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// RulesMatchingNoFiles returns the ids of rules whose scope selected nothing
// out of files, in the order the rules were given.
//
// This is the one place the question is answered. `lint`'s empty-scope check
// and `check`'s scan summary both call it, because the two commands giving
// opposite verdicts about identical state is the defect #151 reports: lint
// FAILed the condition while check printed `[id] OK` at exit 0, one invocation
// apart, from two copies of this loop.
//
// The predicate is config.Rule.Applies, not the prune census: scope.exclude and
// except.paths never reach the scan package, and an include glob that matches
// nothing leaves no trace anywhere — which is why the commonest real cause of a
// vacuous rule, a mistyped include, is invisible to any census-based diagnosis.
//
// External-tool rules are excluded, matching lint's long-standing empty-scope
// exemption: their verdict comes from a shelled-out tool rather than from the
// files their scope selects, so an empty scope does not make them unable to
// fire and reporting them here would be noise.
//
// It does NOT follow that they always run. A `command` rule carrying
// `when.paths_changed` skips silently when no scanned file matched that
// trigger — a separate gate (`whenSpec` / the `sawTrigger` check in
// command.FinalizeErr), not this scope predicate, and #53's shape. An earlier
// version of this comment asserted they "run regardless of what the tree
// holds", which is false in exactly that case. That skip is surfaced by
// SelfSkippedRules below, which asks the checker rather than the scope — still
// not here, because this predicate cannot see it.
func RulesMatchingNoFiles(rls []*config.Rule, files []*scan.File) []string {
	var out []string
	for _, r := range rls {
		if isExternalTool(r) {
			continue
		}
		matched := false
		for _, f := range files {
			if r.Applies(f.Path()) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, r.ID)
		}
	}
	return out
}

// ScopeFloorFindings returns one error-severity finding per rule whose scope
// selected fewer files than its declared scope.min_files floor, in the order the
// rules were given.
//
// It is the ARMING end of the same question RulesMatchingNoFiles above reports.
// That one names a vacuous rule and changes no exit code, because an empty scope
// is legitimate — a rule scoped to a path the repo has not created yet is not a
// defect (#160). A finished port needs the other end: a way to say "this rule
// governs a corpus, and a run where that corpus vanished is a failure, not a
// pass". min_files is that, per rule, defaulting to 0 so every rule that does
// not declare one behaves exactly as it did before the key existed. The shipped
// precedent is set-relation's min_count, whose default 0 keeps empty∩empty green
// while a test-claim rule sets ≥1 so a zero-evidence join cannot pass.
//
// Two deliberate divergences from RulesMatchingNoFiles, both because that
// predicate diagnoses every rule automatically while this one only ever fires on
// a number the operator typed:
//
//   - External-tool rules are NOT exempt. The exemption there exists because a
//     command rule's verdict comes from a shelled-out tool rather than from its
//     scope, so calling it vacuous would be noise. An explicit floor on such a
//     rule is not noise, it is a request, and ignoring it would leave a rule
//     that reads as armed and cannot fail.
//   - The finding is error severity whatever the rule's own severity is. A
//     warn-severity shortfall exits 0, which is an armed floor that cannot fail
//     the run — the exact shape the exit-code contract exists to prevent.
//
// The finding is unexemptable, as spec §5 requires of every scope-level finding.
// The operative reason is that it never enters engine.Run: exemption is applied
// there, to findings the engine produced, and these are appended by the caller
// afterwards. Belt and braces, it also carries no Path, which is what
// engine.suppressAllowlist returns early on, so even routed through the engine
// no allowlist entry could match it.
//
// It counts the file set it is GIVEN and never re-walks, which is what lets the
// caller decide what its mode's floor is a claim about: whole-tree `check` hands
// it the walk, and a --staged/--range run hands it the tracked tree.
func ScopeFloorFindings(rls []*config.Rule, files []*scan.File) []finding.Finding {
	var out []finding.Finding
	for _, r := range rls {
		floor := r.MinFiles()
		if floor <= 0 {
			continue
		}
		n := 0
		for _, f := range files {
			if r.Applies(f.Path()) {
				n++
			}
		}
		if n >= floor {
			continue
		}
		out = append(out, finding.Finding{
			RuleID:   r.ID,
			Severity: finding.SeverityError,
			Message: fmt.Sprintf("scope matched %d file(s), below this rule's declared scope.min_files floor of %d — "+
				"the rule ran against less than the corpus it was armed for; restore the missing files, "+
				"fix the scope globs, or lower scope.min_files if the shrink is intended", n, floor),
		})
	}
	return out
}

// AnyScopeFloor reports whether any of these rules arms a scope floor. It is the
// cheap question `check` asks before paying for a tracked-tree listing in a
// file-set run: a corpus that declared no floor must not acquire a git call on
// the pre-commit path for a feature it does not use.
func AnyScopeFloor(rls []*config.Rule) bool {
	for _, r := range rls {
		if r.MinFiles() > 0 {
			return true
		}
	}
	return false
}

// AnySupersetUnsafe reports whether any rule cannot be judged over a scan that
// pruned LESS than declared (#178).
//
// When scan.gitignore is declared and git cannot answer, nothing is pruned and
// the tree scanned is a superset of the declared one. That was argued
// fail-closed, and for most rules it is: a rule firing on the PRESENCE of
// something can only gain matches from a superset, never lose one.
//
// The argument inverts for a rule that fires on an ABSENCE. A superset can
// supply the very thing whose absence was the violation, and the finding
// disappears — over a scan the operator was told was larger, not smaller. A
// scope floor was the first member found (a floor is a claim about the declared
// corpus that an unpruned tree can satisfy); it is not the only one.
//
// The population is exactly rules.IsWholeTreeInvariant: a verdict that depends
// on the whole scanned set is precisely what a superset breaks. Asked as a
// property rather than a list of rule types, because a list would need
// maintaining and the next absence-asserting type would be added without it.
//
// One-sided in the safe direction. It includes whole-tree invariants whose
// verdict a superset could not actually erase, and refusing there costs a
// diagnosable exit 2 rather than a silent wrong answer.
func AnySupersetUnsafe(rls []*config.Rule) bool {
	for _, r := range rls {
		if r.MinFiles() > 0 || rules.IsWholeTreeInvariant(r.Checker) {
			return true
		}
	}
	return false
}

// SelfSkippedRules returns, for every rule whose CHECKER declined to run itself,
// its id and the checker's reason — in the order the rules were given.
//
// It answers the question RulesMatchingNoFiles above cannot: that one judges a
// rule by the SCOPE predicate, and a `command` rule's when.paths_changed gate is
// invisible to it. So a gate that shelled out to nothing passed both that check
// and the renderer's `[id] OK` (#159).
//
// It must be called AFTER the engine has run: the SkipReporter contract requires
// a checker to answer false until it has actually declined, so over an un-run
// corpus this returns an empty list — a silent one, not a wrong one, which is
// why the caller's ordering is the thing tests have to pin.
func SelfSkippedRules(rls []*config.Rule) []report.SkippedRule {
	var out []report.SkippedRule
	for _, r := range rls {
		// The reason is NOT filtered on being non-empty: a checker that skipped
		// without explaining itself is exactly the one whose skip must not
		// disappear. report.SkippedRule.Line renders that case in words.
		if reason, skipped := rules.SkipReasonOf(r.Checker); skipped {
			out = append(out, report.SkippedRule{
				RuleID: r.ID, Channel: report.SkipChannelSelf, Reason: reason,
			})
		}
	}
	return out
}

// PruneChannels enumerates the declared prune channels with live counts, in
// config order (every scan.ignore glob, then scan.gitignore). Both `check`'s
// scan summary and `lint`'s escape-hatch enumeration render these, so a typo'd
// glob reports "0 matches" from both rather than silently protecting nothing in
// one of them.
//
// Only the two DECLARED channels appear, because a channel here is built per
// config entry and those are the only two an operator declares. Built-in skips
// are pruned without a record, and scope.exclude/except.paths are rule fields
// the scan package never sees — so this is a census of what the operator
// declared, never of everything the walk withheld.
//
// A symlink the walk declined to follow IS recorded (#235) and still does not
// belong here: it is declared nowhere, so there is no glob and no reason to key
// a channel on. UnfollowedLinks below is its peer, and both are rendered by
// `check` and `lint` alike.
func PruneChannels(cfg *config.Config, ignored []scan.Ignored, gi GitIgnoreResult) []report.PruneChannel {
	var out []report.PruneChannel
	for _, e := range cfg.Ignore {
		dirs, files := 0, 0
		for _, ig := range ignored {
			if ig.Glob != e.Glob {
				continue
			}
			if ig.Dir {
				dirs++
			} else {
				files++
			}
		}
		out = append(out, report.PruneChannel{
			Channel: "scan.ignore", Glob: e.Glob, Reason: e.Reason, Dirs: dirs, Files: files,
		})
	}
	if cfg.Gitignore != nil {
		ch := report.PruneChannel{Channel: "scan.gitignore", Reason: cfg.Gitignore.Reason}
		if gi.State == GitIgnoreUnknown {
			// Reported in words, never as a count: "0 matches" asserts that git
			// was asked and said none, and a run where git could not answer must
			// not be able to borrow that sentence.
			//
			// Formatted through %v rather than .Error(), as the code this
			// replaced did: GitIgnoreResult is exported and taken by value, so a
			// caller constructing one with State Unknown and no Err is
			// representable, and printing "<nil>" beats panicking inside a
			// diagnostic whose whole job is to explain a degraded run.
			ch.Undetermined = fmt.Sprintf("%v", gi.Err)
		} else {
			for _, ig := range ignored {
				if ig.By != scan.SourceGitignore {
					continue
				}
				if ig.Dir {
					ch.Dirs++
				} else {
					ch.Files++
				}
			}
		}
		out = append(out, ch)
	}
	return out
}

// UnfollowedLinks names every symlink the walk declined to follow, sorted by
// path, for report.ScanSummary.UnfollowedLinks.
//
// ONE PLACE DECIDES WHICH RECORDS THESE ARE, and that is the point of the
// function rather than an inline loop at each consumer. `check`'s scan summary
// and `lint`'s escape-hatch enumeration are two commands describing the same
// state, and every time this repo has let them each compute it, they came to
// describe it differently (#151). The two callers are internal/cli's summary
// construction and enumerateEscapeHatches.
//
// The sort makes the output deterministic on its own terms rather than by
// inheriting WalkWith's ordering, so a consumer diffing two runs is comparing
// the census and not the walk.
func UnfollowedLinks(ignored []scan.Ignored) []string {
	var out []string
	for _, ig := range ignored {
		if ig.By != scan.SourceSymlink {
			continue
		}
		out = append(out, ig.Path)
	}
	sort.Strings(out)
	return out
}
