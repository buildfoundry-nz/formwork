// Package meta implements formwork lint: self-integrity checks over the
// configuration itself (spec §9). Phase 2 delivered fixture-coverage and
// empty-scope; phase 3a adds exemption-hygiene and the escape-hatch
// enumeration.
package meta

import (
	"fmt"
	"io"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/marker"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// Lint runs the self-integrity checks against root's config and tree,
// writes per-check verdicts plus the escape-hatch enumeration to w, and
// returns how many checks failed. devOptOutActive reports whether
// FORMWORK_ALLOW_DEV is actually letting the RUNNING binary skip a declared
// engine constraint — the gate version that fact depends on lives in package
// cli, which imports this package, so the caller computes it and passes it
// in rather than this package importing cli back (that would be a cycle).
func Lint(cfg *config.Config, root string, w io.Writer, devOptOutActive bool, scoped bool) (int, error) {
	failed := 0
	// Checks run is dynamic: lane-reachability only runs when lanes are
	// configured, so the summary denominator tracks how many actually ran.
	total := 0

	// The corpus's declared skip list (#89), read before anything is emitted so
	// a malformed one is a config error rather than a verdict printed over a
	// check set nobody could parse. Absent file = every check runs.
	pol, err := loadLintPolicy(root)
	if err != nil {
		return 0, err
	}

	// rules-present (#151 row 12). Every other check below iterates cfg.Rules,
	// so a corpus with no rules made all of them report nothing and lint printed
	// an all-OK card — "3/3 checks passed", exit 0 — over a config that enforces
	// nothing at all. That is #133's vacuity one level up: a check that cannot
	// fail is a check that passes.
	//
	// Reported as a CHECK rather than an up-front refusal, and that placement is
	// load-bearing: a refusal in runLint preempted the checks that DO have
	// something to say about a rule-less config, and replaced their itemised
	// output with one line asserting they would all have been vacuous.
	//
	// Which ones actually survive a rule-less config, checked rather than
	// assumed (an earlier version of this comment claimed the escape-hatch
	// census "does not iterate rules at all", which is false — census.go:113
	// loops cfg.Rules for external-tool rules, scope.exclude, except.paths,
	// markers and allowlists):
	//   - lane-nonempty and lane-reachability run off cfg.Lanes and name each
	//     dead lane. This is the one that mattered: a config declaring lanes
	//     with no rules had real, itemised findings that the refusal discarded.
	//   - the census's SCAN-channel half (scan.ignore, scan.gitignore) reads
	//     cfg.Ignore/cfg.Gitignore and reports independently of rules; its
	//     rule-iterating half correctly reports nothing when there are none.
	//   - scan-ignore-tracked walks the tree against cfg.Ignore, not the rules.
	//
	// So the card tells the whole truth: this check fails, and the others still
	// run and still print whatever they found.
	//
	// Skipped under --rule: scopeToRule refuses an id that matches nothing, so a
	// scoped config always carries exactly the rule that matched and this check
	// is structurally incapable of failing. Counting it would put a check that
	// cannot fail into the denominator an operator reads as coverage — the very
	// vacuity the check exists to report (#133). lane-reachability and
	// lane-nonempty are already conditional for the same reason.
	//
	// NOT gated on the skip list: a corpus that could declare this check
	// inapplicable would be exempting itself from the one check that reports its
	// board is empty. loadLintPolicy refuses the entry (exit 2) rather than this
	// call site ignoring it, so the declaration is answered instead of silently
	// having no effect — see nonSkippable in lintpolicy.go.
	if !scoped {
		var rulesPresent []string
		if len(cfg.Rules) == 0 {
			rulesPresent = append(rulesPresent, "no rules are configured — every per-rule check below has nothing to examine, so their OK verdicts prove nothing about this repo")
		}
		failed += emit(w, "rules-present", rulesPresent)
		total++
	}

	// prose-not-truncated (#59): a plain scalar whose text ran past a ' #' lost
	// its tail at parse time. Path-only and content-free — it reads the rule
	// files' raw bytes, which is the only place the evidence still exists — so
	// it runs here, before the tree walk, alongside the other static checks.
	if !pol.skipping(w, "prose-not-truncated") {
		truncated, err := proseTruncated(root)
		if err != nil {
			return 0, err
		}
		failed += emit(w, "prose-not-truncated", truncated)
		total++
	}

	if !pol.skipping(w, "fixture-coverage") {
		if err := fixtureCoverage(cfg, root, w, &failed, &total); err != nil {
			return 0, err
		}
	}

	// Lane reachability (spec §9): when lanes are configured, every rule must
	// be selected by at least one lane that runs in CI — no dead-on-disk
	// rules. When no lanes are configured (e.g. the self-hosted formwork
	// repo), the check is skipped entirely so lint stays clean. This check is
	// purely static, so it runs before the tree scan.
	if len(cfg.Lanes) > 0 {
		if !pol.skipping(w, "lane-reachability") {
			var unreachable []string
			for _, r := range cfg.Rules {
				if !selectedByCILane(cfg.Lanes, r) {
					unreachable = append(unreachable, r.ID+": not selected by any ci lane")
				}
			}
			failed += emit(w, "lane-reachability", unreachable)
			total++
		}

		// A configured lane that selects zero rules is dead config, symmetric
		// to empty-scope rot: flag it so a lane never silently matches nothing.
		if !pol.skipping(w, "lane-nonempty") {
			var dead []string
			for _, l := range cfg.Lanes {
				if !laneSelectsAny(l, cfg.Rules) {
					dead = append(dead, l.Name+": selects no rules")
				}
			}
			failed += emit(w, "lane-nonempty", dead)
			total++
		}
	}

	gi := ResolveGitIgnore(cfg, root)
	fset, err := scan.WalkWith(root, gi.Opts(cfg.IgnoreGlobs()))
	if err != nil {
		return 0, err
	}

	if err := lintTree(cfg, root, w, fset, gi, pol, devOptOutActive, &failed, &total); err != nil {
		return 0, err
	}

	// A declared skip the run never reached is dead config, and dead config in
	// the one file that says "these checks were deliberately not run" is exactly
	// the silence #89 set out to remove. Checked here, on the success path,
	// because a run that returned early legitimately never reached its checks.
	if err := pol.unusedErr(scoped); err != nil {
		return 0, err
	}

	// The floor. Every check above is conditional on something — lanes being
	// configured, a rule carrying a prefilter, the corpus not declaring the check
	// inapplicable — and with enough of those false the summary printed
	// "formwork lint: 0/0 checks passed" and exited 0: a green card over a run
	// that judged nothing, which is the vacuity rules-present reports one level
	// further up again. Reachable only under `--rule`, and not headroom: a scoped
	// run does not report rules-present, so a corpus skipping the three
	// unconditional per-rule checks has an empty board. A whole-corpus run always
	// carries rules-present, which is why the two branches below differ.
	//
	// It is the weakest floor that is exactly true. It does NOT assert that a
	// check capable of FAILING on this config ran, which is the stronger property
	// and would catch a board carrying only checks that cannot fail; that needs
	// each check to answer whether it was falsifiable on this config, which none
	// of them can today. examples/palletra-port-full is that board right now, and
	// its .formwork/lint.yaml says so in the file itself.
	//
	// Under --rule the floor reports instead of refusing, for the same reason
	// unusedErr disarms there: narrowing to one rule is the operator asking a
	// question this corpus may simply have no check for, and a valid config plus
	// a valid rule id must not be exit 2 (the contract reserves that for an
	// engine or config error). `lint --rule <id>` on examples/palletra-port-full
	// was exit 2 for exactly that reason — rules-present is whole-corpus, and the
	// corpus declares the other four inapplicable, so the board is empty through
	// no fault of the config.
	//
	// It still must not read as a pass, and the wording is the whole of that: it
	// says no check ran, never "0/0 checks passed". An empty board reported as an
	// empty board is information; reported as a green card it is the vacuity this
	// floor exists to stop.
	if total == 0 {
		if scoped {
			fmt.Fprintf(w, "formwork lint: no lint check ran%s — nothing here judges this rule\n", pol.summarySuffix())
			return 0, nil
		}
		// Not reachable from any config: rules-present is unconditional and
		// cannot be skipped, so a whole-corpus board always carries at least it.
		// Kept as an invariant assertion rather than deleted — if that ever stops
		// being true, this is the difference between finding out and printing a
		// green card over nothing. It is deliberately unpinned by a test, because
		// no config can construct the state it guards.
		return 0, fmt.Errorf("lint: no lint check ran%s — rules-present is unconditional, so an empty whole-corpus board means the engine did not run the checks it owes", pol.summarySuffix())
	}

	fmt.Fprintf(w, "formwork lint: %d/%d checks passed%s\n", total-failed, total, pol.summarySuffix())
	return failed, nil
}

// lintTree runs every check whose question is about the SCANNED TREE, in the
// order an operator reads them: the path-only checks first, then the refusal
// that stops lint judging a tree it cannot read, then the ones that draw on file
// content. Split out of Lint, which the 750-line vendor cap bounds.
func lintTree(cfg *config.Config, root string, w io.Writer, fset *scan.FileSet, gi GitIgnoreResult, pol *lintPolicy, devOptOutActive bool, failed, total *int) error {
	// scan-ignore-tracked (#90): scan.ignore pruning is sound only while the
	// pruned paths stay uncommitted — a tracked file under a pruned glob is
	// invisible to every rule at once and rides through commit, review, and
	// CI with a green check. Primary match: git's tracked set against the
	// walk's own prune records (fold-aware on core.ignorecase repos, where
	// the index spelling can differ from disk by case alone). Fallback for
	// paths the walk never saw (deleted tree, sparse checkout — states that
	// persist, they are not transient): match the index path and its
	// ancestors against the globs directly; the walk made no decision for
	// these paths, so no disagreement with the scan is possible, and the
	// built-in-skip exclusion is re-established via scan.UnderBuiltinSkip.
	// Runs only when scan.ignore is configured, and only when the corpus has not
	// declared the check skipped. WHEN IT RUNS, any git failure — including "not
	// a git repository" — is an engine error, never a silent skip (vcs package
	// contract). A declared skip stops the check's work, so it also stops that
	// refusal: exit 2 becomes exit 0 here and in fixture-coverage, which is what
	// gating the WORK rather than the output line costs, and is deliberate —
	// a skipped check that still computed could fail the run with an error, which
	// would make the skip a lie about what lint did. The skip is printed on every
	// run and named in the summary, so the trade is disclosed rather than silent.
	if len(cfg.Ignore) > 0 && !pol.skipping(w, "scan-ignore-tracked") {
		tracked, err := vcs.TrackedUnder(root)
		if err != nil {
			enumerateEscapeHatches(cfg, root, nil, fset, gi, devOptOutActive, w)
			return err
		}
		// GetConfigBool errors when the key is unset; unset means git did not
		// detect a case-folding filesystem, so exact matching is correct. A
		// malformed boolean cannot hide in that error: git parses core config
		// at startup, so TrackedUnder above would already have failed on it.
		fold := false
		if v, err := vcs.GetConfigBool(root, "core.ignorecase"); err == nil && v {
			fold = true
		}
		inScan := make(map[string]bool, len(fset.Files))
		for _, f := range fset.Files {
			inScan[f.Path()] = true
		}
		var hidden []string
		for _, p := range tracked {
			g, hid := ignoredByFold(p, fset.Ignored, fold)
			if !hid && !inScan[p] && !scan.UnderBuiltinSkip(p) {
				g, hid = matchesIgnoreGlob(p, cfg.IgnoreGlobs())
			}
			if hid {
				hidden = append(hidden, fmt.Sprintf("%s: tracked but hidden from every rule by scan.ignore (%s) — untrack it or narrow the glob", p, g))
			}
		}
		*failed += emit(w, "scan-ignore-tracked", hidden)
		*total++
	}

	// empty-scope FIRES ON ABSENCE, which is what makes the unresolved-gitignore
	// fallback unsafe here rather than merely degraded.
	//
	// ResolveGitIgnore's Unknown state prunes NOTHING and scans the whole tree.
	// For a check that fires on the presence of something that is a superset and
	// cannot manufacture a pass. This one fails when a rule's scope matches no
	// files, so the very files the fallback declined to prune are what make the
	// absence go away. Measured, on a rule scoped to a gitignored gen/: healthy
	// run reports `[empty-scope] FAIL`, exit 1; with the fallback in force,
	// `[empty-scope] OK` and `formwork lint: 4/4 checks passed`, exit 0 — the
	// sentence this check exists to prevent.
	//
	// So it refuses instead, at exit 2, the same posture `rules-for` already takes
	// on this state (cli/rulesfor.go) and the same one `check` now takes for
	// scope.min_files, which is the other absence check in the engine. It returns
	// rather than emitting a problem: a problem is a verdict about the corpus, and
	// the point is that the corpus could not be determined.
	//
	// The checks ABOVE have already emitted, deliberately — they are static or
	// git-free and their answers stand. This is scan-ignore-tracked's precedent a
	// few lines up, which likewise enumerates the escape hatches before returning
	// a git error.
	if gi.State == GitIgnoreUnknown {
		enumerateEscapeHatches(cfg, root, nil, fset, gi, devOptOutActive, w)
		return fmt.Errorf("scan.gitignore is declared but git could not answer it (%v), so empty-scope cannot be judged: nothing was pruned, and the files that would have been are exactly the ones that could make an empty scope look populated", gi.Err)
	}

	if !pol.skipping(w, "empty-scope") {
		var empty []string
		byID := make(map[string]*config.Rule, len(cfg.Rules))
		for _, r := range cfg.Rules {
			byID[r.ID] = r
		}
		for _, id := range RulesMatchingNoFiles(cfg.Rules, fset.Files) {
			msg := id + ": scope matches no files in this repo"
			// Say WHY when the answer is the engine skip set (#56). Without
			// this, a correct-looking glob under .formwork/ matches nothing and
			// the operator has no way to learn that the walk never goes there —
			// the skip appears in no rule's scope.exclude and cannot.
			if r, ok := byID[id]; ok && scan.ScopeRootedUnderSkipDir(r.Include()) {
				msg += fmt.Sprintf(" — every include is rooted under a directory the engine never scans (%s), so no path the walk produces can match",
					strings.Join(scan.BuiltinSkipDirs(), ", "))
			}
			empty = append(empty, msg)
		}
		*failed += emit(w, "empty-scope", empty)
		*total++
	}

	// command-trigger-armable (#161): a `command` rule's tool runs only when a
	// file the rule's own scope let through matches when.paths_changed, so a
	// trigger no path in this repository can satisfy is a gate that never fires
	// — on any commit, in any mode. empty-scope cannot see it (the scope may be
	// populated), the escape-hatch census names the rule without qualifying it,
	// and the per-run skip disclosure (#159) reports one run rather than the
	// permanent condition. Conditional, like the lane checks: it enters the
	// denominator only when some rule declares a trigger.
	if anyTriggerGated(cfg.Rules) && !pol.skipping(w, "command-trigger-armable") {
		*failed += emit(w, "command-trigger-armable", commandTriggerProblems(cfg.Rules, fset.Files))
		*total++
	}

	// The engine evaluation is only run when something actually consumes its
	// results: allowlist staleness needs real findings to compare against, the
	// escape-hatch enumeration's suppressed-finding listing (G1) needs them,
	// and the prefilter-load-bearing check below reuses them for its base pass.
	// A repo with no allowlist and no marker opt-in anywhere has nothing for
	// the engine to tell lint that its own static checks don't already know
	// (D2, efficiency). A run error is HELD, not surfaced here: the
	// prefilter-load-bearing check must still emit its verdict first, so a
	// load-bearing-prefilter diagnostic is never hidden behind an unrelated
	// engine error (e.g. a command rule whose external tool is missing) —
	// visibility must not regress on a degraded repo (D1).
	engineRules := rulesFeedingLint(cfg.Rules)
	mainRan := len(engineRules) > 0
	var findings []finding.Finding
	var mainErr error
	if mainRan {
		findings, mainErr = engine.Run(engineRules, fset, 0)
	}

	// Prefilter load-bearing (spec §9): a prefilter is a pure optimization, so
	// evaluating a rule with vs without it must not change its findings. Runs
	// only when a rule carries a prefilter (conditional, like the lane checks).
	// Reuses the main run's findings for the as-written (base) side only when it
	// SUCCEEDED — on a main-run error it runs its own base pass, so a nil finding
	// set from a failed run is never mistaken for "base found nothing".
	if anyPrefiltered(cfg.Rules) && !pol.skipping(w, "prefilter-load-bearing") {
		plb, _, err := prefilterLoadBearing(cfg, root, fset, findings, mainRan && mainErr == nil)
		if err != nil {
			// Fail-closed: surface the engine error as exit 2, but print the
			// escape-hatch enumeration first so "nothing is silently excluded"
			// holds on a degraded repo.
			enumerateEscapeHatches(cfg, root, findings, fset, gi, devOptOutActive, w)
			return err
		}
		*failed += emit(w, "prefilter-load-bearing", plb)
		*total++
	}

	// Now surface a held main-run error (exit 2), still printing the
	// escape-hatch enumeration first so "nothing is silently excluded" holds on
	// a degraded repo (D1).
	if mainErr != nil {
		enumerateEscapeHatches(cfg, root, findings, fset, gi, devOptOutActive, w)
		return mainErr
	}

	// #30: whether lint failed closed on an unreadable in-scope file used to
	// depend on which checks happened to run. With no allowlist, no marker and
	// no prefilter, nothing consumed engine findings, so lint read no file and
	// passed over a 0o000 one; add a `prefilter:` — which the spec calls a PURE
	// OPTIMIZATION that must never change a verdict — and the same repo exited 2.
	//
	// One refusal, taken whatever else ran. A file some rule governs but lint
	// cannot read is a rule that is not enforced, and that is never a pass.
	//
	// The ALTITUDE is the boundary between the two kinds of check, not the top
	// of the function: everything above answers from paths alone and its answers
	// stand on a degraded tree, while everything below draws on file CONTENT.
	// Placed higher it would suppress verdicts that are still valid — the
	// prefilter differential's own diagnostic among them (D1) — and placed lower
	// each content check would keep discovering the read error for itself, which
	// is the contingency this replaces.
	//
	// The probe reads through scan.File.Content, the same cached read the engine
	// uses, so it gives the identical answer and a subsequent engine run pays
	// nothing for it. On a repo whose checks never needed the engine that is a
	// new whole-tree read; uniformity is worth it, and the alternative — a
	// cheaper os.Open probe — would leave a mid-read error tolerated in exactly
	// the configuration this issue is about. Deliberately NOT skippable via
	// .formwork/lint.yaml: it is not a check with a verdict, it is the
	// precondition for having one.
	if err := refuseUnreadableInScope(cfg.Rules, fset.Files); err != nil {
		enumerateEscapeHatches(cfg, root, findings, fset, gi, devOptOutActive, w)
		return err
	}

	if !pol.skipping(w, "exemption-hygiene") {
		hygiene, err := exemptionHygiene(cfg, fset, findings)
		if err != nil {
			enumerateEscapeHatches(cfg, root, findings, fset, gi, devOptOutActive, w)
			return err
		}
		*failed += emit(w, "exemption-hygiene", hygiene)
		*total++
	}

	enumerateEscapeHatches(cfg, root, findings, fset, gi, devOptOutActive, w)
	return nil
}

// selectedByCILane reports whether any lane that runs in CI selects r.
func selectedByCILane(lanes []config.Lane, r *config.Rule) bool {
	for _, l := range lanes {
		if l.CI && l.Selects(r) {
			return true
		}
	}
	return false
}

// laneSelectsAny reports whether lane l selects at least one rule.
func laneSelectsAny(l config.Lane, rls []*config.Rule) bool {
	for _, r := range rls {
		if l.Selects(r) {
			return true
		}
	}
	return false
}

// rulesFeedingLint returns exactly the rules whose engine findings lint reads,
// which is a strict subset of the config and is what the engine is run over.
//
// Three consumers, and nothing else looks at the finding set:
//   - allowlist staleness — needs findings for allowlist-carrying rules;
//   - the escape-hatch enumeration's suppressed-finding listing — only ever has
//     anything to show for marker- or allowlist-exempt rules;
//   - prefilterLoadBearing — reuses these findings as the as-written base for
//     every prefilter-carrying rule, so those must be included or that check
//     compares a stripped run against a base that never contained the rule.
//     An empty base is indistinguishable from "the prefilter changed nothing",
//     which would turn the one check that proves prefilters are pure
//     optimizations into a false green. Its `reuseMain` argument is the seam
//     that would otherwise have to carry a partial-findings signal; including
//     the rules keeps that contract intact instead.
//
// Running the whole config here — the previous behaviour, an ANY-predicate
// gating engine.Run(cfg.Rules) — meant ONE opted-in rule anywhere dispatched
// EVERY command/git-diff escape. Those are enforcement, and enforcement is
// `formwork check`'s job; lint reads nothing they produce. In the validating
// target that cost a second whole-tree resolved-Dart-AST pass (~1,570 files,
// plus a Flutter toolchain and a workspace pub get) on every PR, docs-only
// included (buildfoundry-nz/formwork#64).
//
// Selecting rules rather than filtering findings is deliberate: a rule that is
// never handed to the engine cannot execute, whereas dropping its findings
// afterwards would have paid the whole cost first.
func rulesFeedingLint(rls []*config.Rule) []*config.Rule {
	var out []*config.Rule
	for _, r := range rls {
		if r.Allowlist != nil || r.Marker {
			out = append(out, r)
			continue
		}
		if _, ok := rules.PrefilterOf(r.Checker); ok {
			out = append(out, r)
		}
	}
	return out
}

func emit(w io.Writer, check string, problems []string) int {
	if len(problems) == 0 {
		fmt.Fprintf(w, "[%s] OK\n", check)
		return 0
	}
	fmt.Fprintf(w, "[%s] FAIL — %d problem(s)\n", check, len(problems))
	for _, p := range problems {
		fmt.Fprintf(w, "  %s\n", p)
	}
	return 1
}

// exemptionHygiene reports stale allowlist entries, reasonless markers, and
// dead scope.exclude entries that lack a YAML justification comment.
// findings is the engine's evaluation of rulesFeedingLint(cfg.Rules) over fset
// (nil when nothing in the config would consume a finding); suppressed findings
// count as "still trips" (phase-3a design §4). The subset always contains every
// allowlist- and marker-carrying rule, which is exactly what this check reads,
// so narrowing the run does not narrow what hygiene can see.
//
// Dead-exclude hygiene: an exclude that matches zero files in the
// scanned tree is an exemption nobody can currently exercise. Fail it only
// when the YAML entry also has no comment — a commented dead exclude is the
// deliberate preventative shape (vendor/build/generated trees that may be
// absent on disk). Enumeration of every exclude lives in
// enumerateEscapeHatches; this arm is the vacuity half only.
func exemptionHygiene(cfg *config.Config, fset *scan.FileSet, findings []finding.Finding) ([]string, error) {
	type key struct{ rule, path string }
	trips := map[key]bool{}
	for _, f := range findings {
		trips[key{f.RuleID, f.Path}] = true
	}
	exists := map[string]bool{}
	for _, f := range fset.Files {
		exists[f.Path()] = true
	}

	// D3: one pass over every in-scope file's lines (prefiltered by a plain
	// substring check) rather than a separate rule × file × line scan per
	// marker-enabled rule; reasonlessByRule groups the results back by rule
	// id so the loop below can emit them in the same rule-ID order as before.
	reasonlessByRule, err := reasonlessMarkersByRule(cfg.Rules, fset)
	if err != nil {
		return nil, err
	}

	var problems []string
	for _, r := range cfg.Rules {
		if al := r.Allowlist; al != nil {
			for _, e := range al.Entries {
				switch {
				case !exists[e.Path]:
					// "does not exist" would be false for a file that is on
					// disk but hidden by a prune — same failure verdict
					// (the exemption can never fire), truthful diagnosis. Each
					// channel is named as itself: the cure differs (narrow a
					// glob in .formwork vs. an entry in .gitignore), so a
					// reader sent to the wrong file learns nothing.
					//
					// The third channel is the SCAN'S OWN SPELLING, and it is
					// asked first because it is the only affirmative answer
					// here: the other two say which mechanism removed the file,
					// this one says the scan read it. `exists` is an exact
					// string map, and on macOS/APFS an entry an editor saved
					// NFC names a directory entry readdir returns NFD — a miss
					// that read as absence and sent the operator to create a
					// file they are looking at (#308's class, at this caller).
					//
					// ASKED, NOT RECOMPUTED. scan.(*FileSet).Produced owns this
					// question on the filesystem's device+inode oracle, and a
					// second copy of that oracle on this side of the package
					// boundary is exactly the divergence #151 is about. Its
					// cost is one pass over Files, reached only for an entry
					// that is already a problem — a healthy corpus has none.
					//
					// THE VERDICT DOES NOT MOVE. The engine compares allowlist
					// paths with `==` (engine.go:479), so the entry still
					// cannot suppress anything and is still reported; calling
					// it live here would certify an exemption the run ignores.
					if fset.Produced(e.Path) {
						problems = append(problems, fmt.Sprintf("%s: allowlist %s:%d: %s is spelled differently on disk (unicode normalization) — allowlist paths are matched byte-for-byte, so exemption is inert; re-add the entry with the bytes the scan carries", r.ID, al.File, e.Line, e.Path))
					} else if g, ok := ignoredBy(e.Path, fset.Ignored); ok {
						problems = append(problems, fmt.Sprintf("%s: allowlist %s:%d: %s hidden by scan.ignore (%s) — exemption is inert", r.ID, al.File, e.Line, e.Path, g))
					} else if rec, ok := hidingRecord(e.Path, fset.Ignored, false, scan.SourceGitignore); ok {
						problems = append(problems, fmt.Sprintf("%s: allowlist %s:%d: %s hidden by scan.gitignore (%s) — exemption is inert", r.ID, al.File, e.Line, e.Path, rec.Rule))
					} else {
						problems = append(problems, fmt.Sprintf("%s: allowlist %s:%d: %s does not exist", r.ID, al.File, e.Line, e.Path))
					}
				case !trips[key{r.ID, e.Path}]:
					problems = append(problems, fmt.Sprintf("%s: allowlist %s:%d: %s no longer trips the rule (stale)", r.ID, al.File, e.Line, e.Path))
				}
			}
		}
		// scope.exclude: fail a dead (matches-zero-files) entry with no
		// justification comment. Live excludes need no comment; commented
		// dead excludes are load-bearing preventative carve-outs, not rot.
		for _, e := range r.ExcludeEntries() {
			if excludeMatchesAny(e.Glob, fset) {
				continue
			}
			if e.Comment != "" {
				continue
			}
			problems = append(problems, fmt.Sprintf("%s: scope.exclude %q matches no files and has no justification comment", r.ID, e.Glob))
		}
		// A rule that hasn't opted into markers (`except: {marker: true}`)
		// never honors formwork:allow for it either way, so "missing a
		// reason" would be misleading — adding one still wouldn't exempt
		// anything. Those markers fall into the deferred stale-marker bucket
		// instead (phase-3a carryover notes).
		if !r.Marker {
			continue
		}
		problems = append(problems, reasonlessByRule[r.ID]...)
	}
	return problems, nil
}

// ignoredBy reports which scan.ignore glob (if any) hides path: a file-level
// match, or any ancestor recorded as a pruned dir (the walk never descended,
// so descendants of a pruned dir have no records of their own).
func ignoredBy(path string, ignored []scan.Ignored) (string, bool) {
	return ignoredByFold(path, ignored, false)
}

// ignoredByFold is ignoredBy with optional case folding, for core.ignorecase
// repositories where the index spelling of a path can legitimately differ
// from the on-disk (walk) spelling by case alone (#90). The folded prefix
// compare slices at byte length, so it is sound for ASCII/length-preserving
// case pairs — the realistic APFS divergence; a fold that changes encoded
// length (ſ→s) can still miss, a disclosed residual beside NFC/NFD (spec §9).
// It answers for the scan.ignore channel ONLY. The scan-ignore-tracked check
// names that channel in its message and prescribes narrowing the glob, so a
// gitignore prune must not be reported through it — and cannot legitimately
// arrive here anyway: git refuses to call a tracked path ignored, which is the
// property the whole gitignore channel rests on. Filtering makes that explicit
// rather than relying on it silently.
func ignoredByFold(path string, ignored []scan.Ignored, fold bool) (string, bool) {
	if rec, ok := hidingRecord(path, ignored, fold, scan.SourceIgnore); ok {
		return rec.Glob, true
	}
	return "", false
}

// hidingRecord returns the walk record from the named channel that hides path,
// matching a Dir record against the path and everything beneath it.
func hidingRecord(path string, ignored []scan.Ignored, fold bool, by scan.Source) (scan.Ignored, bool) {
	eq := func(a, b string) bool { return a == b }
	hasPrefix := strings.HasPrefix
	if fold {
		eq = strings.EqualFold
		hasPrefix = func(s, prefix string) bool {
			return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
		}
	}
	for _, ig := range ignored {
		if ig.By != by {
			continue
		}
		if ig.Dir && (eq(path, ig.Path) || hasPrefix(path, ig.Path+"/")) {
			return ig, true
		}
		if !ig.Dir && eq(path, ig.Path) {
			return ig, true
		}
	}
	return scan.Ignored{}, false
}

// matchesIgnoreGlob is the record-free fallback for the scan-ignore-tracked
// check. The walk-faithful matching (level-outer, glob-inner — #95 review)
// and its case-fold disclosure (spec §9) live on scan.IgnoredBy, which this
// delegates to so lint and rules-for (#108) attribute identically.
func matchesIgnoreGlob(path string, globs []string) (string, bool) {
	return scan.IgnoredBy(path, globs)
}

// excludeMatchesAny reports whether glob matches any path in fset. Uses the
// same doublestar matcher config.Rule.Applies uses for scope.exclude, so the
// hygiene verdict agrees with the engine's own scope evaluation.
func excludeMatchesAny(glob string, fset *scan.FileSet) bool {
	for _, f := range fset.Files {
		// Match's only error is ErrBadPattern; config.New already validated
		// every stored exclude glob at load, so the error branch is
		// unreachable here the same way it is inside config.matchAny.
		if ok, err := doublestar.Match(glob, f.Path()); err == nil && ok {
			return true
		}
	}
	return false
}

// reasonlessMarkersByRule scans every in-scope file once: a
// strings.Contains prefilter skips lines that couldn't possibly carry a
// formwork:allow marker at all, and a qualifying line is classified once per
// marker-enabled rule whose scope applies to that file (internal/marker
// owns the grammar both this and the engine use). Results are grouped by
// rule id, in the order the file/line scan encountered them, so callers can
// reproduce the previous per-rule, in-file-order, in-line-order output.
func reasonlessMarkersByRule(rls []*config.Rule, fset *scan.FileSet) (map[string][]string, error) {
	var markerRules []*config.Rule
	for _, r := range rls {
		if r.Marker {
			markerRules = append(markerRules, r)
		}
	}
	if len(markerRules) == 0 {
		return nil, nil
	}

	problems := map[string][]string{}
	for _, f := range fset.Files {
		var applicable []*config.Rule
		for _, r := range markerRules {
			if r.Applies(f.Path()) {
				applicable = append(applicable, r)
			}
		}
		if len(applicable) == 0 {
			continue
		}
		lines, err := f.Lines()
		if err != nil {
			return nil, err
		}
		for i, line := range lines {
			if !strings.Contains(line, "formwork:allow") {
				continue
			}
			for _, r := range applicable {
				if marker.Classify(line, r.ID) == marker.Reasonless {
					problems[r.ID] = append(problems[r.ID], fmt.Sprintf("%s: %s:%d: formwork:allow marker missing a reason", r.ID, f.Path(), i+1))
				}
			}
		}
	}
	return problems, nil
}
