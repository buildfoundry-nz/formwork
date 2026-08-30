// Package cli wires config, scan, engine, and report behind the formwork
// command surface and owns the exit-code contract (spec §6).
package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	"github.com/buildfoundry-nz/formwork/internal/report"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"

	// Register the built-in rule types.
	_ "github.com/buildfoundry-nz/formwork/internal/rules/baseline"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/binarycontent"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/dartscan"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/docpathexists"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filesize"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/ordering"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pairconsistency"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/patterncount"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/setrelation"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
)

// Run executes the CLI and returns the process exit code:
// 0 pass, 1 error-severity findings, 2 usage/config/engine error.
func Run(args []string, stdout, stderr io.Writer) int {
	// FORMWORK_GIT_ENV is resolved ONCE, here, before any subcommand runs.
	// internal/vcs decides what the variable means and enforces it at the git
	// exec sites; this is only where the answer is printed, because that package
	// writes to no stream of its own.
	//
	// Before dispatch rather than inside the git-using subcommands: the
	// operator's environment is misconfigured, or the hatch is in force,
	// independently of which command was typed — and a refusal that depended on
	// whether the subcommand happened to reach git would be silent for exactly
	// the invocations that report on configuration.
	gitEnvNotice, err := vcs.GitEnvNotice()
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return 2
	}
	if gitEnvNotice != "" {
		fmt.Fprintln(stderr, "formwork:", gitEnvNotice)
	}
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "test":
		return runTest(args[1:], stdout, stderr)
	case "lint":
		return runLint(args[1:], stdout, stderr)
	case "scope":
		return runScope(args[1:], stdout, stderr)
	case "hooks":
		return runHooks(args[1:], stdout, stderr)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr)
	case "rules-for":
		return runRulesFor(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "formwork "+displayVersion())
		return 0
	default:
		fmt.Fprintf(stderr, "formwork: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: formwork <command> [flags]")
	fmt.Fprintln(w, "commands: check, test, lint, scope, hooks, explain, list, rules-for, version")
}

// newFlagSet builds a FlagSet for a subcommand with the shared "-C" root
// flag already registered. Parse errors are written to stderr by the flag
// package itself (via SetOutput); callers still parse via parseAndLoad.
func newFlagSet(name, rootUsage string, stderr io.Writer) (fs *flag.FlagSet, root *string) {
	fs = flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	root = fs.String("C", ".", rootUsage)
	return fs, root
}

// parseAndLoad parses args into fs and, on success, loads the config at
// *root. On a flag-parse error or config-load error it reports the failure
// (config errors are prefixed "formwork:") and returns ok=false; callers
// should return exit code 2 in that case.
//
// The engine-version gate runs here, after fs.Parse but before rule files are
// parsed: config.ReadEnvelope reads .formwork/formwork.yaml exactly once,
// enforceEngine gates on that same parsed envelope, and only then does
// env.LoadRules parse rule files — from the identical bytes the gate just
// evaluated, not a second, possibly-different read of the file (finding 8).
// A binary that does not satisfy a declared `engine:` constraint reports
// that, instead of failing on rule-file schema it was never built to
// understand (spec §4). Every command that loads config goes through
// parseAndLoad or loadGated (the introspection commands, which validate
// their own flags first), so the gate covers check, test, lint, scope,
// hooks, explain, list, and rules-for alike — a config the binary must
// refuse is refused before ANY of its content is rendered as guidance
// (#119 review finding 1).
func parseAndLoad(fs *flag.FlagSet, args []string, root *string, stderr io.Writer) (cfg *config.Config, ok bool) {
	if err := fs.Parse(args); err != nil {
		return nil, false
	}
	return loadGated(*root, stderr)
}

// loadGated is parseAndLoad's envelope-gate-load half: one envelope read,
// the engine gate on those same bytes, then rule loading.
func loadGated(root string, stderr io.Writer) (cfg *config.Config, ok bool) {
	env, err := config.ReadEnvelope(root)
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return nil, false
	}
	if !enforceEngine(env.Engine, env.EngineConstraint, stderr) {
		return nil, false
	}
	cfg, err = env.LoadRules()
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return nil, false
	}
	return cfg, true
}

// scopeToRule returns a shallow copy of cfg narrowed to the single rule named
// by id, or ok=false if no rule matches — the caller must then exit 2. A
// selector that silently matches nothing and reads as "all clear" is exactly
// the misconfigured-rule-passes failure the exit-code contract exists to
// prevent. Lanes are dropped in the copy: lane-level integrity checks
// (reachability, non-emptiness) are whole-config and meaningless when scoped to
// one rule, and a lane that no longer selects the retained rule would otherwise
// false-fail lane-nonempty. The original cfg is left untouched.
func scopeToRule(cfg *config.Config, id string) (scoped *config.Config, ok bool) {
	var kept []*config.Rule
	for _, r := range cfg.Rules {
		if r.ID == id {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil, false
	}
	c := *cfg
	c.Rules = kept
	c.Lanes = nil
	return &c, true
}

// rangeValueUsable reports whether --range is safe to act on, printing the
// refusal itself when it is not. fallback names what the caller would silently
// do instead, so the message says what the operator would have lost.
//
// An explicitly-passed empty --range is a SUPPLIED flag, not an absent one, and
// every caller here decides between range and staged mode with `*rangeSpec !=
// ""` — which cannot tell the two apart. So `--range ”` (an unset $BASE..$HEAD
// in a CI wrapper, a shallow clone, a skipped base-ref step) silently becomes a
// different run: a whole-tree scan in check, and in scope a classification of
// the STAGED set, which in CI is empty. That used to reach `docs`, the weakest
// class, at exit 0; since #147 the empty set is announced and assumed runtime,
// so what survives here is a supplied flag quietly answering a different
// question — worth refusing, but no longer a silent gate bypass.
//
// It lives here rather than inline because the first cut of this guard went into
// runCheck alone, and runScope — same file, same flag, same fallback — kept the
// old behaviour, so the two commands gave opposite answers to identical input.
// A shared helper is what stops a third caller diverging again.
func rangeValueUsable(fs *flag.FlagSet, rangeSpec, fallback string, stderr io.Writer) bool {
	given := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "range" {
			given = true
		}
	})
	if given && rangeSpec == "" {
		fmt.Fprintf(stderr, "formwork: --range was given an empty value — refusing to fall back to %s; pass a real range (e.g. origin/main..HEAD) or drop the flag\n", fallback)
		return false
	}
	return true
}

// workersValueUsable reports whether --workers is safe to act on, printing the
// refusal itself when it is not. Same shape as rangeValueUsable above and for
// the same reason: a supplied flag whose value the run cannot honour must not
// quietly become a different run.
//
// engine.Run reads `workers <= 0` as GOMAXPROCS, which is the right default for
// an ABSENT flag and is left exactly as it is. What that seam cannot see is
// whether the number it was handed is a default or a value the CLI declined to
// honour — a width is all it receives — so the distinction is drawn here, at the
// seam that knows the flag exists and can name it in the refusal. Both commands
// that declare the flag call this before doing anything with the value; `test`
// spends it one hop further away (fixturetest.Run forwards it to engine.Run,
// run.go:155), which is exactly why the guard is not left to the engine.
//
// No fs.Visit, unlike the --range guard, and the difference is in the values
// rather than in the shape: both declaration sites default this flag to 0 (the
// `fs.Int("workers", 0, ...)` calls in runCheck and runTest), and 0 is also a
// legal supplied value meaning the same thing — so absent and supplied-0 are
// genuinely indistinguishable AND genuinely identical, while a NEGATIVE value
// can only have been typed. Refusing 0 would exit 2 on every ordinary
// invocation, which is a worse bug than the one this guard closes.
func workersValueUsable(workers int, stderr io.Writer) bool {
	if workers < 0 {
		fmt.Fprintf(stderr, "formwork: --workers %d is not a worker count — refusing to fall back to GOMAXPROCS, which is the opposite of the throttle you asked for; pass a positive count, or drop the flag\n", workers)
		return false
	}
	return true
}

// noRulesReason explains a rule set that is empty, naming the cause rather than
// guessing at it. consequence is the caller's half of the sentence — what this
// particular command would have done with nothing to run.
//
// The cause matters because the cure differs and the message is the entire cure
// surface of the refusal. An absent .formwork/rules and a rules file that parses
// but declares nothing (`rules: []`, or a null `rules:` key — what a bad merge or
// a templating error leaves behind) are different mistakes, and telling the
// second operator to "add a rule file" sends them to fix the thing they are
// already looking at.
//
// One owner for this wording, deliberately: it was copy-pasted across three
// commands, docs/quickstart.md quotes it verbatim, and nothing tied the copies
// together — so rewording it meant editing four places and getting two different
// explanations of one condition if you missed one.
func noRulesReason(cfg *config.Config, consequence string) string {
	if cfg.RuleFiles == 0 {
		return "no rules are configured — .formwork/rules holds no *.yaml files, so " + consequence +
			"; add a rule file, or point -C at the repository you meant"
	}
	return fmt.Sprintf("no rules are configured — %d rule file(s) in .formwork/rules parsed but declared no rules "+
		"(an empty or null `rules:` list), so %s; check those files, or point -C at the repository you meant",
		cfg.RuleFiles, consequence)
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("check", "repository root to check", stderr)
	workers := fs.Int("workers", 0, "worker count (0 or absent = GOMAXPROCS; a negative count is refused)")
	lane := fs.String("lane", "", "run only the named lane's rules (default: all rules)")
	staged := fs.Bool("staged", false, "scan only git-staged files (mutually exclusive with --range)")
	rangeSpec := fs.String("range", "", "scan only files changed in a git range, e.g. origin/main..HEAD")
	skipEscapes := fs.Bool("skip-escapes", false, "skip heavy command/git-diff escapes (they re-scan the whole tree regardless of --staged; run them in CI, not local hooks)")
	format := fs.String("format", "human", "output format: human | json | github")
	cfg, ok := parseAndLoad(fs, args, root, stderr)
	if !ok {
		return 2
	}
	// FLAG validation comes before anything about config CONTENT: a caller who
	// mistyped a flag needs to hear about the flag, not about their rule set.
	// (The zero-rules guard below preempted this once — both are exit 2, so the
	// contract held, but the message named the wrong problem and
	// TestCheckStagedAndRangeMutuallyExclusive caught it.)
	//
	// -format is validated here too, not just the two flags that started it: it
	// used to be checked inside report.Render, after the guard and after the
	// whole scan, so `-format bogus` on a rule-less repo answered a typo with
	// advice about rules.
	//
	// --workers joined them in #156: a negative value used to be accepted and
	// discarded (engine.Run treats workers <= 0 as GOMAXPROCS), the same
	// supplied-flag-silently-ignored shape the --range guard below rejects.
	//
	// Still NOT a claim of completeness — the previous version of this comment
	// made one it did not have, which is why it was rewritten to name the gap
	// instead. What is asserted here is only what the four checks below do, in
	// their order: --workers, --staged/--range exclusivity, an empty --range,
	// and -format.
	if !workersValueUsable(*workers, stderr) {
		return 2
	}
	if *staged && *rangeSpec != "" {
		fmt.Fprintln(stderr, "formwork: --staged and --range are mutually exclusive")
		return 2
	}
	if !rangeValueUsable(fs, *rangeSpec, "a whole-tree scan", stderr) {
		return 2
	}
	if err := report.ValidFormat(*format); err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return 2
	}
	// --lane selects which rules run; --staged/--range select which files.
	// Unknown lane → config error (exit 2).
	rls := cfg.Rules
	configured := len(rls)
	if *lane != "" {
		selected, err := cfg.RulesForLane(*lane)
		if err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		rls = selected
	}
	selectedCount := len(rls)
	// Fast local hooks: the heavy escapes (command / git-diff) shell out to a
	// script that re-scans the whole tree, so --staged cannot make them cheap —
	// drop them here and let the whole-tree CI run be their backstop. The
	// declarative rules over the staged files are milliseconds. This narrows
	// what a LOCAL run covers, so a repo that cares must keep the escapes on
	// its CI lane; formwork itself cannot tell where it is running.
	//
	// The drop is named in the scan summary, not silent. It is legitimate and
	// stays exit 0, but the dropped rule never reaches the engine — so its
	// checker cannot report the skip, and an empty not-run section would read as
	// "every rule ran". Dropping ALL escapes is already a named exit 2 below;
	// dropping SOME must not be quieter than that. Lane selection deliberately
	// gets no such line: a lane not choosing a rule is selection working as
	// asked, not a rule being dropped out from under the run.
	var droppedEscapes []report.SkippedRule
	if *skipEscapes {
		kept := rls[:0:0]
		for _, r := range rls {
			if r.Cost() != rules.CostHeavy {
				kept = append(kept, r)
				continue
			}
			reason := fmt.Sprintf("did not run: --skip-escapes dropped this heavy %s rule; the whole-tree CI run is its backstop", r.Type)
			// The rule's scope floor goes with it, and that is disclosed rather
			// than evaluated. The cost argument for the drop does not reach a
			// floor — it is glob matching over a file set already in hand — but
			// emitting its finding here would exit 1 having printed nothing:
			// report.Human renders findings by iterating the rules it was handed,
			// and this one is no longer among them. A silent failure is worse than
			// a disclosed gap (#23, fix round 1).
			if floor := r.MinFiles(); floor > 0 {
				reason += fmt.Sprintf(" — its scope.min_files floor of %d went unevaluated with it", floor)
			}
			droppedEscapes = append(droppedEscapes, report.SkippedRule{
				RuleID: r.ID, Channel: report.SkipChannelSkipEscapes, Reason: reason,
			})
		}
		rls = kept
	}
	// A run with NO RULES to run evaluates nothing, and the renderer's
	// "0/0 rules passed, 0 finding(s)" reads exactly like a clean tree — the
	// #151 rows 10-12 fail-open. It is a config error, not a pass: the engine
	// was handed nothing to enforce, so it learned nothing about this tree.
	//
	// The selector a few lines above is ALREADY fail-closed for an unknown lane
	// (exit 2). A lane that names no EXISTING rule and a lane that resolves to
	// zero rules are the same hazard wearing different spellings, so they get
	// the same answer. Same posture as this repo's other zero-target refusals:
	// internal/publication/manifest.go ("declares nothing — refusing a vacuous pass"),
	// the Makefile's sync/sync-status manifest guard, and sync-manifest-proof.sh.
	//
	// The cause is named because all three are curable and the cures differ.
	// Note the ordering: `configured` is captured before any filter runs, so an
	// empty .formwork/rules is never misreported as a lane or --skip-escapes
	// problem.
	if len(rls) == 0 {
		var reason string
		switch {
		case configured == 0:
			reason = noRulesReason(cfg, "this run would check nothing")
		case selectedCount == 0:
			reason = fmt.Sprintf("lane %q selects no rules — every rule was filtered out by the lane's tags/cost, so this run would check nothing (formwork lint's lane-nonempty check reports the same condition)", *lane)
		case *skipEscapes:
			reason = fmt.Sprintf("--skip-escapes dropped all %d selected rule(s) — every one is a heavy command/git-diff escape, so this run would check nothing; drop the flag, or wire a lane that carries at least one fast rule", selectedCount)
		default:
			// Unreachable today: --skip-escapes is the only filter between
			// selectedCount and here. Naming skip-escapes unconditionally would
			// have been sound now and a confident lie the moment a third filter
			// lands, which is how a message outlives the code it describes.
			reason = fmt.Sprintf("no rules are left to run: %d configured, %d selected, all filtered out before the scan", configured, selectedCount)
		}
		fmt.Fprintln(stderr, "formwork:", reason)
		return 2
	}
	// scan.gitignore prunes paths git itself refuses to track. When it is
	// declared but git cannot answer, NOTHING is pruned and the reason is
	// printed: the resulting scan is a superset of the declared one, so for every
	// check that fires on the PRESENCE of something this direction cannot let a
	// rule pass that would otherwise fail, but it is still a departure from what
	// the config asked for and must not be silent (spec §11 — never skip
	// silently, and never silently DECLINE to skip).
	//
	// A SUPERSET IS NOT SAFE FOR A CHECK THAT FIRES ON ABSENCE, and scope.min_files
	// is exactly that: it fails when the scope matched TOO FEW files, so files the
	// fallback declined to prune can satisfy a floor the declared corpus does not.
	// Measured — a rule armed at 3 over a src/ holding 2 matching files plus a
	// gitignored gen/ holding 5: healthy run exits 1 on the floor, the same tree
	// with the fallback in force reports `[needs-corpus] OK`, exit 0. The floor
	// exists to turn a shrunken scope from a disclosure into a verdict (#23), and
	// this route silently gave the verdict back. So it is refused where a floor is
	// armed and left as it was everywhere else; `rules-for` already takes this
	// posture on the same state (rulesfor.go), and which is owed depends on whether
	// the answer can be given without the pruning.
	//
	// NOT IN A FILE-SET MODE, for two reasons, the second being why the refusal
	// would be DEAD rather than merely unnecessary. The floor is measured there
	// against the TRACKED tree (below), which git reports directly and no pruning
	// inflates — so the fallback cannot manufacture a pass. And for the cause that
	// brought this guard here, an ambient git environment moving the repository, a
	// file-set run has ALREADY exited 2: trackedFileSet calls vcs.TrackedPaths →
	// vcs.EnsureTopLevel, whose git invocation runs through the same environment
	// guard. That second reason covers only that cause — a git answering `ls-files`
	// but not `ls-files --others --ignored` still reaches here — which is what the
	// first is for, and it holds for all of them.
	gi := meta.ResolveGitIgnore(cfg, *root)
	if gi.State == meta.GitIgnoreUnknown {
		fmt.Fprintf(stderr, "formwork: scan.gitignore: could not determine — %v; nothing pruned, scanning the whole tree\n", gi.Err)
		if !*staged && *rangeSpec == "" && meta.AnySupersetUnsafe(rls) {
			fmt.Fprintf(stderr, "formwork: refusing to judge this corpus over that scan: the files scan.gitignore would have pruned are unknown, so the tree scanned is a SUPERSET of the declared one. A rule that fires on an ABSENCE — a scope floor, a whole-tree invariant — can have its finding erased by a file that should not have been there, over a scan you were told was larger rather than smaller. Fix the git error above, or drop scope.min_files and the whole-tree-invariant rule(s) from this run\n")
			return 2
		}
	}
	// opts is held rather than inlined because the file-set guard below has to
	// ask the SAME question the walk answered — which channel hides this path —
	// and a second, separately-built Opts could answer it differently.
	opts := gi.Opts(cfg.IgnoreGlobs())
	fset, err := scan.WalkWith(*root, opts)
	if err != nil {
		fmt.Fprintln(stderr, "formwork: scan:", err)
		return 2
	}
	// The scan summary reports what the run LOOKED AT, alongside what it found
	// (#151). FilesScanned and RulesMatchingNoFiles are recomputed below from
	// the file sets the engine is actually handed, so they describe the run that
	// happened rather than a whole-tree run that may not have.
	//
	// Prunes is deliberately NOT recomputed: a prune is a property of the WALK,
	// not of the changeset. Restricting it would report "0 matches" for a glob
	// that is fully live merely because this commit did not touch the tree it
	// hides, which is the opposite of what the census is for.
	//
	// UnfollowedLinks is the walk's Ignored on the same reasoning, and it is a
	// PEER of Prunes rather than one of them: PruneChannels builds a channel per
	// DECLARED config entry, and a link the walk declined to follow is declared
	// nowhere, so it has nothing to key on (meta.PruneChannels and
	// internal/meta/census.go both say so from their own side). Without this the
	// record reached `formwork lint` alone, and `check` — the command the
	// installed pre-commit shim and every CI lane run — reported 0 findings at
	// exit 0 over a path it never opened (#309).
	summary := report.ScanSummary{
		FilesScanned:    len(fset.Files),
		Prunes:          meta.PruneChannels(cfg, fset.Ignored, gi),
		UnfollowedLinks: meta.UnfollowedLinks(fset.Ignored),
	}
	// The file set an armed scope floor is measured against. Whole-tree mode
	// counts the walk — that is exactly what the engine scanned — and the
	// file-set branch narrows it to the tracked tree below.
	floorFiles := fset.Files
	// File-set modes restrict the scan to a git changeset. Fail-closed: a git
	// error is exit 2, never a silent fall back to the whole tree.
	var findings []finding.Finding
	if *staged || *rangeSpec != "" {
		// scannableOnly, not anyStatus: everything below opens these paths, so a
		// deleted one has nothing to read. scope asks the same seam the other
		// way; see changesetStatuses.
		changed, err := changesetFor(*root, *staged, *rangeSpec, scannableOnly)
		if err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		allow := make(map[string]bool, len(changed))
		for _, p := range changed {
			allow[p] = true
		}
		changedFset := fset.Restrict(allow)
		// Every path git named must be accounted for: scanned, or absent for a
		// reason the WALK owns. #151 row 9 and #158 are the two ways it was not —
		// a declared prune channel removed the path, or the path was never on
		// disk to remove — and both printed the same "1/1 rules passed" as a run
		// with nothing staged. There is no benign reading of either, so both are
		// exit 2, the same answer rangeValueUsable gives one screen up for a
		// supplied flag that silently becomes a different run.
		//
		// It reports here rather than after the engine runs because a refusal
		// that arrives with a rendered "rules passed" report has already told the
		// operator the opposite of what it means. The classification, the
		// precedence between the two causes and the wording each gets all live in
		// fileset.go, next to the accounting they read.
		flagName := "--staged"
		if !*staged {
			flagName = "--range " + *rangeSpec
		}
		refuse, refuseErr := refuseUnaccountedPaths(stderr, *root, flagName, *rangeSpec, *staged, changed, changedFset, opts)
		if refuseErr != nil {
			fmt.Fprintln(stderr, "formwork:", refuseErr)
			return 2
		}
		if refuse {
			return 2
		}

		// Partition by monotonicity (#4). A whole-repo INVARIANT
		// (required-pattern exists / set-relation / pattern-count / baseline) is
		// non-monotonic under file removal: judging it on the changed files
		// alone false-fails it as "not found" / "subset violated" whenever the
		// file bearing its token is outside the range. Such rules therefore
		// evaluate over the tracked tree even here — matching CI's whole-tree
		// run. Everything else is per-file/monotonic and range-scopes to the
		// changeset for speed (removing files can only remove its findings).
		var invariant, perFile []*config.Rule
		for _, r := range rls {
			if r.WholeTreeInvariant() {
				invariant = append(invariant, r)
			} else {
				perFile = append(perFile, r)
			}
		}
		// Under a file-set mode the summary describes the RESTRICTED set — that
		// is what --staged/--range asked about — and it reports BOTH counts.
		// One of them alone cannot tell an empty request from an unseen one:
		// "0 file(s) scanned" was identical for a run with nothing staged and a
		// run whose every staged path the walk failed to produce.
		//
		// Vacuity is deliberately NOT computed here. A rule that does not cover
		// this commit is not vacuous, it is irrelevant to it, and naming every
		// such rule would bury the real signal on the path that runs most — the
		// pre-commit shim. It is also the frame in which the summary's referral
		// to `formwork lint` would be false, since lint judges the whole tree.
		summary.FilesScanned = len(changedFset.Files)
		summary.PathsRequested = scannablePaths(changed)
		summary.FileSetMode = flagName
		summary.InvariantRules = len(invariant)
		if len(perFile) > 0 {
			perFindings, perErr := engine.Run(perFile, changedFset, *workers)
			if perErr != nil {
				fmt.Fprintln(stderr, "formwork:", perErr)
				return 2
			}
			findings = append(findings, perFindings...)
		}
		// The TRACKED tree, not the whole working tree, for the two consumers
		// that need it here: an untracked file the developer is not committing
		// must not false-fail a pre-commit invariant (#4), and it must not
		// SATISFY a scope floor either (#23) — one asymmetry read from two
		// sides. Materialised once, and only when something asks for it, so a
		// corpus with neither does not acquire an additional `git ls-files` for
		// a feature it does not use. This path is already in git — it reached
		// here through StagedPaths/RangePaths — so what the gate saves is that
		// one extra call, not a git-free run.
		var trackedFset *scan.FileSet
		if len(invariant) > 0 || meta.AnyScopeFloor(rls) {
			trackedFset, err = trackedFileSet(*root, fset)
			if err != nil {
				fmt.Fprintln(stderr, "formwork:", err)
				return 2
			}
			floorFiles = trackedFset.Files
		}
		if len(invariant) > 0 {
			invFindings, invErr := engine.Run(invariant, trackedFset, *workers)
			if invErr != nil {
				fmt.Fprintln(stderr, "formwork:", invErr)
				return 2
			}
			findings = append(findings, invFindings...)
		}
	} else {
		summary.RulesMatchingNoFiles = meta.RulesMatchingNoFiles(rls, fset.Files)
		findings, err = engine.Run(rls, fset, *workers)
		if err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
	}
	// A rule that ARMED scope.min_files and matched fewer files than it declared
	// fails the run (#23). This is the arming end of the vacuity RulesMatchingNoFiles
	// merely discloses: an empty scope stays a pass by default, and a floor is the
	// per-rule opt-in that makes it a verdict.
	//
	// Never the changeset — floorFiles is the walk, or the tracked tree under
	// --staged/--range. A floor is a claim about the repository, so judging it on
	// the changed files would false-fail every armed rule on every commit, and
	// skipping it in a file-set mode would let the pre-commit shim pass what CI
	// fails. The tracked restriction is what makes that second sentence true: the
	// walk sees files git will not commit, so counting them let an untracked
	// corpus meet a floor locally and miss it in a fresh clone — the very
	// divergence this paragraph claims to close (fix round 1).
	//
	// One Sort owns ordering for both branches now: engine.Run returns its own
	// findings sorted, but these arrive after it in every mode.
	findings = append(findings, meta.ScopeFloorFindings(rls, floorFiles)...)
	finding.Sort(findings)
	// Rules whose CHECKER declined to run, asked once both engine.Run branches
	// above are done — a checker reports a skip only after taking it, so asking
	// earlier would report none (#159). Collected here rather than through
	// engine.Run: the state lives on the rules this function already holds, and
	// engine.Run's four other call sites — lint's evaluation, its two prefilter
	// differentials, and the fixture runner — have no scan summary to put it in,
	// so widening that signature would reach two subsystems for one report.
	//
	// Computed in BOTH modes, unlike RulesMatchingNoFiles above: a file-set run
	// is where a paths_changed trigger goes unmatched, so suppressing it there
	// would suppress it where it fires.
	//
	// That claim covers --staged/--range runs that CARRY heavy rules — a CI
	// lane, or a bare `check --staged`. It is deliberately NOT a claim about the
	// generated pre-commit shim: hooks.shim runs `check --lane <lane>`, adding
	// --staged for pre-commit alone (hooks.fileSetFlag), and a hook lane is
	// conventionally cost: fast (examples/quickstart declares pre-commit exactly
	// that way), which drops every command rule before the engine — so no
	// trigger-skip can arise on that path to be disclosed.
	//
	// The --skip-escapes drops come first and from the unfiltered pass above:
	// those rules are gone from rls by now, so nothing downstream could recover
	// them.
	summary.RulesNotRun = append(droppedEscapes, meta.SelfSkippedRules(rls)...)
	// rls must be the complete loaded rule set: the json/github renderers
	// join each finding's cure: from it by rule id (#107), so passing a
	// filtered slice would silently render cure-less annotations. Both
	// engine.Run branches above draw findings from this same rls.
	if err := report.Render(*format, stdout, rls, findings, summary); err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return 2
	}
	for _, f := range finding.Unsuppressed(findings) {
		if f.Severity == finding.SeverityError {
			return 1
		}
	}
	return 0
}

func runTest(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("test", "repository root", stderr)
	workers := fs.Int("workers", 0, "worker count (0 or absent = GOMAXPROCS; a negative count is refused)")
	rule := fs.String("rule", "", "run only the named rule's fixtures (default: all rules)")
	cfg, ok := parseAndLoad(fs, args, root, stderr)
	if !ok {
		return 2
	}
	// Ahead of every config-CONTENT guard below, for runCheck's reason: a caller
	// who mistyped a flag needs to hear about the flag, and the zero-rules
	// refusal further down would otherwise answer `--workers -1` with advice
	// about rule files.
	if !workersValueUsable(*workers, stderr) {
		return 2
	}
	// Collect the full id set BEFORE --rule scoping: the orphan check inside
	// Run compares fixture dirs against every rule the corpus declares, so a
	// scoped run must not read sibling rules' fixtures as orphans (#58).
	allIDs := make([]string, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		allIDs = append(allIDs, r.ID)
	}
	// Zero rules CONFIGURED means there is nothing this command could report on,
	// so "0/0 rules passed, 0 fixture(s) run" at exit 0 is a green verdict over
	// nothing (#151 row 12).
	//
	// But the refusal must not TAKE the better diagnosis with it. Run's first
	// act is the orphan-fixture check — fixture trees on disk matching no rule
	// id — and it returns before printing anything, so calling it here with a
	// discarding writer surfaces that specific, actionable error (it names the
	// dead trees) for the case where rule files vanished but their fixtures did
	// not. Guarding ahead of Run instead replaced "these 3 fixture dirs can
	// never run" with "there are no fixtures", which was both less useful and
	// untrue. The discarded writer costs nothing: with zero rules the per-rule
	// loop after the orphan check has nothing to iterate.
	//
	// Deliberately NOT the same condition as "zero fixtures ran". A configured
	// rule with no fixture directory also prints "0/0 rules passed" at exit 0 —
	// but it prints `[id] SKIP — no fixtures` and a skipped count first, and
	// lint's fixture-coverage check fails on it. That one is disclosed, not
	// silent, which is the whole difference.
	if len(cfg.Rules) == 0 {
		if _, err := fixturetest.Run(cfg, allIDs, *root, *workers, io.Discard); err != nil {
			fmt.Fprintln(stderr, "formwork:", err)
			return 2
		}
		fmt.Fprintln(stderr, "formwork:", noRulesReason(cfg, "there are no fixtures to run and nothing this command could report on"))
		return 2
	}
	if *rule != "" {
		scoped, matched := scopeToRule(cfg, *rule)
		if !matched {
			fmt.Fprintf(stderr, "formwork: no rule matches --rule %q\n", *rule)
			return 2
		}
		cfg = scoped
	}
	failed, err := fixturetest.Run(cfg, allIDs, *root, *workers, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return 2
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func runLint(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("lint", "repository root", stderr)
	rule := fs.String("rule", "", "run only the named rule's per-rule checks (default: all rules; skips whole-config lane checks)")
	cfg, ok := parseAndLoad(fs, args, root, stderr)
	if !ok {
		return 2
	}
	scopedToRule := *rule != ""
	if scopedToRule {
		scoped, matched := scopeToRule(cfg, *rule)
		if !matched {
			fmt.Fprintf(stderr, "formwork: no rule matches --rule %q\n", *rule)
			return 2
		}
		cfg = scoped
	}
	failed, err := meta.Lint(cfg, *root, stdout, devOptOutActive(), scopedToRule)
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return 2
	}
	if failed > 0 {
		return 1
	}
	return 0
}
