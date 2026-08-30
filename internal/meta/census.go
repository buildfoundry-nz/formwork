// census.go — the escape-hatch enumeration (spec §9). Split from lint.go,
// which the 750-line vendor cap bounds; same package.
package meta

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/report"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// enumerateEscapeHatches lists every exemption channel per rule — spec §9's
// visibility requirement — plus, per rule and after its other lines, every
// currently suppressed finding the engine evaluation discovered (G1: an
// exemption quietly doing its job must be as visible as one that is merely
// configured). findings is nil when the engine wasn't run (D2) or errored
// before producing results (D1); either way the suppressed-finding lines
// are simply omitted, since there's nothing to report. Informational: never
// a failed check. command-type rules join this block in phase 5.
func enumerateEscapeHatches(cfg *config.Config, root string, findings []finding.Finding, fset *scan.FileSet, gi GitIgnoreResult, devOptOutActive bool, w io.Writer) {
	ignored := fset.Ignored
	suppressedByRule := map[string][]finding.Finding{}
	for _, f := range findings {
		if f.Suppressed {
			suppressedByRule[f.RuleID] = append(suppressedByRule[f.RuleID], f)
		}
	}

	var lines []string
	// The engine-version backstop (spec §4) is itself an escape hatch when it
	// is actively disabled: FORMWORK_ALLOW_DEV lets a dev build skip a
	// declared `engine:` constraint entirely. Surface it only while it is
	// doing something real to THIS binary — no constraint configured means
	// the opt-out is inert, and devOptOutActive is false whenever the running
	// binary is a trusted release (its gate version is never "dev") even if
	// the env var itself parses truthy, so a released binary never gets a
	// false DISABLED claim here (finding 3; spec §9's "nothing is silently
	// excluded" cuts both ways — no false negatives, no false positives).
	if cfg.EngineConstraint != nil && devOptOutActive {
		lines = append(lines, fmt.Sprintf("  engine-version backstop: DISABLED via FORMWORK_ALLOW_DEV=%s", os.Getenv("FORMWORK_ALLOW_DEV")))
	}
	// Symlinks the walk declined to follow (#143). These have NO declaration
	// anywhere — no glob, no .gitignore line — so PruneChannels cannot render
	// them: it builds a channel per declared config entry, and there is nothing
	// declared to key on. That absence is exactly why they need a line. An
	// operator whose rule scopes `**/*.yaml` over a `config.yaml` symlink sees
	// the rule match nothing and is told their glob is wrong; the truth is that
	// the walk declined to look, and nothing said so.
	//
	// Selected by UnfollowedLinks and worded by report.UnfollowedLinkLine, both
	// shared with `check`'s scan summary (#309). Neither the set nor the
	// sentence is computed here any more: this line and check's used to be the
	// same disclosure written twice, and the version `check` did not have was
	// the whole of #309.
	for _, path := range UnfollowedLinks(ignored) {
		lines = append(lines, "  "+report.UnfollowedLinkLine(path))
	}

	// In-scope files outside version control (#80). A file can be present on
	// disk, governed by a rule, and untracked, with every instrument reading
	// clean: the walk reads the filesystem — correctly, since deferring to the
	// index would be a fail-open change, an untracked .go file being compiled by
	// `go build ./...` all the same — and nothing consulted the index at all.
	//
	// DETECTOR, NEVER AUTHORITY. Nothing here changes what is scanned; the
	// census gains a line and no file gains or loses a rule. That direction is
	// the design: a skip could be used to hide something, a census line cannot.
	// TestUntrackedFileIsStillScanned pins it from the other side.
	//
	// Reported per rule, using Applies rather than scope.include alone, so the
	// number means "a rule would have evaluated this" rather than "a glob
	// matched it" — an excluded or carved-out path is not a coverage gap.
	//
	// When git cannot answer, WHICH SILENCE IS OWED DEPENDS ON WHETHER THIS IS A
	// REPOSITORY, and the two must not read alike (#292, restoring #80's
	// fail-closed criterion).
	//
	// Outside a repository the question is not meaningful. "Which in-scope files
	// are outside version control" has no answer in a tree that is not a
	// repository: it is not that the answer is unknown, it is that the
	// distinction does not exist there. Printing "could not determine" on every
	// non-git tree — `formwork test -C examples/...` among them — would assert a
	// gap that is not there, so that case still says nothing.
	//
	// INSIDE a repository the question has an answer and git did not give it. An
	// unreadable or corrupt .git/index, or no git on PATH at all — an ordinary
	// build-container state — used to reach the same silence, and the census then
	// printed the affirmative "escape hatches: none" at exit 0 over a repository
	// nobody had been able to ask. That output was byte-identical to a tree with
	// no repository at all: an operator could not tell "I asked and there are
	// none" from "I could not ask", which is this repo's signature defect and the
	// exact false clean #80 called out. It gets the "could not determine — <err>"
	// idiom the same binary already emits for scan.gitignore (report.PruneChannel
	// .Line, internal/cli/cli.go), and because it is a line rather than an
	// absence, "escape hatches: none" becomes unreachable whenever the census
	// could not be taken.
	//
	// No verdict changes. This is a census line, not a check: nothing passes or
	// fails because of what it reports. The channels that DO need git for a
	// verdict — scan-ignore-tracked above all — refuse at exit 2 when it does not
	// answer, and that refusal is unaffected either way.
	tracked, trackErr := vcs.TrackedUnder(fset.Root)
	switch {
	case trackErr != nil && insideRepository(fset.Root):
		lines = append(lines, fmt.Sprintf("  scan: untracked in-scope files: could not determine — %v; nothing reported (this tree IS a git repository, so the answer is unknown rather than absent)", trackErr))
	case trackErr == nil:
		isTracked := make(map[string]bool, len(tracked))
		for _, p := range tracked {
			isTracked[p] = true
		}
		seen := map[string]bool{}
		var untracked []string
		for _, r := range cfg.Rules {
			for _, f := range fset.Files {
				p := f.Path()
				if isTracked[p] || seen[p] || !r.Applies(p) {
					continue
				}
				seen[p] = true
				untracked = append(untracked, fmt.Sprintf("  %s: untracked in-scope file: %s (scanned, but not in version control)", r.ID, p))
			}
		}
		sort.Strings(untracked)
		lines = append(lines, untracked...)
	}

	// The prune channels hide paths from EVERY rule at once, so they are
	// enumerated ahead of the per-rule lines (the engine-version backstop, when
	// actively disabled, precedes them) and unconditionally-when-configured, with live match
	// counts: a typo'd glob shows "0 matches" forever instead of silently
	// protecting nothing. Dir prunes report as dirs, never as file counts — the
	// walk did not descend, and inventing a number would cost what the prune
	// saved (#56, supersedes #79). Declaring EITHER channel counts as
	// operator-tuning the skip surface, so the built-in-skip context line rides
	// along for both.
	// UNCONDITIONAL (#56). This used to ride along only when the operator had
	// declared a scan.ignore or scan.gitignore, so a repo declaring neither —
	// the common case — never learned the skip existed. It is the one exemption
	// channel no rule can declare in its own scope.exclude, because it is an
	// engine precondition rather than a per-rule choice, which makes the census
	// the only place it is auditable at all. Downstream, 28 rules declared
	// coverage the scanner never gives and the repo's docs asserted a ban
	// "anywhere, no exemptions" that was false for .formwork/.
	//
	// Named from scan.BuiltinSkipDirs rather than restated in prose: the old
	// line hardcoded ".git, .formwork" and would have drifted silently the
	// moment the set changed.
	engineSkips := fmt.Sprintf("engine: never scans directories named %s (any depth) — not an operator choice, and declarable in no rule's scope.exclude",
		strings.Join(scan.BuiltinSkipDirs(), ", "))
	// scan.gitignore is the second channel that hides paths from EVERY rule at
	// once, and unlike scan.ignore its declaration lives OUTSIDE .formwork/ —
	// in .gitignore, which no governance scope covers. That is the strongest
	// objection to the whole mechanism (#80), and its line here is the answer
	// to it: the channel is named with live counts every time lint runs, so
	// what git hides is auditable from the same place every other exemption is.
	//
	// Both channels are rendered by report.PruneChannel.Line, which `check`'s
	// scan summary also calls: describing the same channel two different ways
	// in the two commands is the shape of the defect this shares a fix with
	// (#151). PruneChannels emits nothing for an undeclared channel, so an
	// unconfigured repo keeps its "escape hatches: none" contract.
	for _, ch := range PruneChannels(cfg, ignored, gi) {
		lines = append(lines, "  "+ch.Line())
	}
	for _, r := range cfg.Rules {
		// External-tool rules (command, git-diff) shell out and are exempt from
		// fixture-coverage/empty-scope; surface every one here so a shell-out —
		// and the fixture exemption — is never invisible (spec §5/§9).
		if isExternalTool(r) {
			// WHICH DECISION WAS MADE, not merely that one exists (#230).
			// This line used to read "fixture-exempt" unconditionally. That
			// names the lint POLICY correctly — heavy rules are exempt from
			// fixture-coverage — but it was emitted identically for a rule
			// carrying a complete, passing fire/pass proof and for a rule
			// carrying nothing, on the rule type #230 measures as the least
			// proven and the most exercised. A disclosure surface that cannot
			// tell those apart discloses nothing about proof.
			//
			// Both fixtures are named because for a heavy rule the PAIR is the
			// proof and neither half is one alone: a detector that cannot run
			// exits non-zero in the fire fixture too, so only the pass fixture
			// shows it executed (#240).
			proof := "NO firing proof: no fixtures"
			switch fire, pass, ferr := fixtureCounts(filepath.Join(root, ".formwork", "fixtures", r.ID)); {
			case ferr != nil:
				// Unreadable is not provable. Saying "no fixtures" here would
				// report a filesystem error as a verdict about the rule.
				proof = "firing proof UNKNOWN: " + ferr.Error()
			case fire > 0 && pass > 0:
				proof = "proved by fire+pass fixtures"
			case fire > 0:
				proof = "NO firing proof: fire fixture only, nothing shows the detector ran"
			case pass > 0:
				proof = "NO firing proof: pass fixture only, nothing shows it can fire"
			}
			line := fmt.Sprintf("  %s: %s rule (external tool, heavy — %s)", r.ID, r.Type, proof)
			// #161: naming the rule as an escape hatch, unqualified, reads as
			// "this tool runs and formwork does not police it". A when: gate
			// makes that half true — the tool runs only when a file in THIS
			// RULE'S SCOPE matches the trigger, which is the condition
			// command-trigger-armable judges and which the reader of this line
			// cannot see from the rule id alone.
			if globs, ok := triggerGlobsOf(r.Checker); ok {
				line += fmt.Sprintf("; gated by when.paths_changed (%s) — runs only when a file in this rule's scope matches", strings.Join(globs, ", "))
			}
			lines = append(lines, line)
		}
		// scope.exclude is an exemption channel just like except.paths: it
		// carves files out of a rule while the rest of the fleet reports
		// clean. Enumerate every entry so it is never invisible.
		for _, g := range r.Exclude() {
			lines = append(lines, fmt.Sprintf("  %s: scope.exclude: %s", r.ID, g))
		}
		// except.paths reports what each entry REMOVED, not what the rule
		// declares (#138). The two neighbouring channels already report effect —
		// scan.ignore prints live match counts so a typo'd glob reads "0
		// matches" forever, and suppressed findings are enumerated per rule —
		// and this was the one that printed a fossil with the same weight as a
		// live carve-out.
		//
		// It is also the channel where the gap cannot be closed by reading
		// findings: except.paths is a scope SUBTRACTION, so the rule never
		// evaluates the file and no finding exists to count. The file set is the
		// only witness, which is why this function takes it.
		if paths := r.ExceptPaths; len(paths) > 0 {
			removed := make(map[string]int, len(paths))
			for _, f := range fset.Files {
				if g, ok := r.CarvedOutBy(f.Path()); ok {
					removed[g]++
				}
			}
			// The SECOND number, which is the one that ranks the entries
			// (#138). "N files removed" alone cannot tell a load-bearing
			// carve-out from a fossil: an entry that removes three files the
			// rule would never fire on renders exactly like one removing the
			// single file it would. `0 would fire` is this channel's version of
			// the `0 matches` a typo'd scan.ignore glob already gets.
			//
			// IT CANNOT COME FROM THE FINDINGS, which is what makes except.paths
			// different from every other exemption here. It is a scope
			// SUBTRACTION, not a suppression: Rule.Applies returns false, the
			// rule never evaluates the file, and no finding exists to count. The
			// number has to be produced by evaluating the rule against the
			// carved-out files on purpose, which is what wouldFire does.
			would, wErr := wouldFireUnder(r, fset)
			for _, g := range paths {
				line := fmt.Sprintf("  %s: except.paths: %s: %d file(s) removed from this rule's evaluation", r.ID, g, removed[g])
				switch {
				case wErr != nil:
					line += fmt.Sprintf(", would fire: unknown (%v)", wErr)
				case would == nil:
					line += ", would fire: unknown (whole-run rule — its verdict depends on the set it was given, so a per-file answer would be invented)"
				default:
					line += fmt.Sprintf(", %d would fire", would[g])
				}
				lines = append(lines, line)
			}
		}
		// SQL the extractor could not read (#75). sqlextract.FromGo returns
		// unresolved Sites — the honest record of "there is SQL here and I could
		// not read it" — and both rule-side consumers discarded them, so every
		// composition it cannot model vanished with no finding and no
		// diagnostic. Spec §9's coverage limits (strings.Builder, loop/switch
		// composition, a named closure's appends, goto, taken-address writes)
		// were disclosed only as prose in a doc comment.
		//
		// That made it the one exemption channel the census did not cover, and
		// an unreadable query is exactly a silent exclusion. #55 is the
		// precedent: it made scope.exclude countable rather than leaving it
		// silent.
		//
		// Scoped to files THIS rule governs, not the whole tree: a Go file no
		// SQL rule covers is not a gap in the SQL gate, and reporting it would
		// bury the signal. Cost is a Go parse per in-scope file, paid by lint
		// and not by check.
		// Routed through sqlparse.CensusSites, not a prefix match plus FromGo
		// (#311). The locking types source their SQL through
		// FromGoReassembled; asking FromGo about them made the census answer
		// about a different extraction than the rule fires on — it called the
		// exact line `check` had just failed on "not analysed by this rule",
		// and stayed silent about compositions the rule really cannot read.
		// CensusSites keys on a table of registered types, so a sql/* type in
		// neither table is refused rather than handed FromGo's answer.
		for _, f := range fset.Files {
			if !strings.HasSuffix(f.Path(), ".go") || !r.Applies(f.Path()) {
				continue
			}
			content, err := f.Content()
			if err != nil {
				continue
			}
			sites, ok, err := sqlparse.CensusSites(r.Type, f.Path(), content)
			if !ok {
				break // not a SQL rule; no file of its scope will be either
			}
			if err != nil {
				continue // a Go parse failure is the rule's to report, not the census's
			}
			{
				for _, s := range sites {
					lines = append(lines, fmt.Sprintf("  %s: SQL at %s:%d could not be read (%s) — not analysed by this rule",
						r.ID, s.Path, s.Line, s.Reason))
				}
			}
		}

		// A declared fixture exemption is an escape hatch: it removes a heavy
		// rule's firing proof, so it is enumerated with its reason (#53).
		//
		// Only when it is actually in force (#336). Two ways it is not, and
		// both were enumerated anyway:
		//
		//   - Content-free. `fixture_exempt: "   "` renders as
		//     `id: fixture-exempt (declared):` with nothing after the colon.
		//     The census exists to name WHICH decision was made (#230); a line
		//     naming none discloses nothing. Same TrimSpace predicate
		//     fixture-coverage now uses to refuse it.
		//
		//   - On a fast rule. `fixture_exempt` governs heavy rules only
		//     (docs/reference.md:186) — fixture-coverage judges a fast rule on
		//     its fire/pass fixtures regardless. So the census claimed an
		//     exemption was in force two lines below fixture-coverage failing
		//     that same rule for the fixtures the exemption did not buy.
		if strings.TrimSpace(r.FixtureExempt) != "" && isExternalTool(r) {
			lines = append(lines, fmt.Sprintf("  %s: fixture-exempt (declared): %s", r.ID, r.FixtureExempt))
		}
		if r.Marker {
			if _, ok := r.Checker.(rules.Finalizer); ok {
				// Marker suppression only ever applies to per-file checker
				// findings (spec §5); a checker that implements Finalizer
				// can emit findings markers can never reach. Dual-role
				// checkers (e.g. required-pattern in every-file mode) also
				// satisfy this type assertion even though markers work for
				// their per-file findings — this annotation is a visibility
				// hint, not a correctness claim (phase-3a carryover, B2).
				lines = append(lines, fmt.Sprintf("  %s: marker enabled (finalizer findings: allowlist-only)", r.ID))
			} else {
				lines = append(lines, fmt.Sprintf("  %s: marker enabled", r.ID))
			}
		}
		if al := r.Allowlist; al != nil {
			lines = append(lines, fmt.Sprintf("  %s: allowlist %s (%d entries)", r.ID, al.File, len(al.Entries)))
		}
		// findings is already sorted by (rule id, path, line) — see
		// engine.Run — so this subsequence is already deterministically
		// ordered by path then line without needing to sort again.
		for _, f := range suppressedByRule[r.ID] {
			if f.Line == 0 {
				lines = append(lines, fmt.Sprintf("  %s: suppressed %s (%s)", r.ID, f.Path, f.SuppressedBy))
			} else {
				lines = append(lines, fmt.Sprintf("  %s: suppressed %s:%d (%s)", r.ID, f.Path, f.Line, f.SuppressedBy))
			}
		}
	}
	// Printed ABOVE the block rather than inside it. The engine skip set is a
	// precondition present in every repository, so listing it as an escape-hatch
	// entry would make "escape hatches: none" unreachable — and that line is a
	// real signal, meaning this repo declares no exemptions of its own. #56 asks
	// for the skip set to be visible in the census; it does not ask for it to be
	// counted as something an operator chose.
	fmt.Fprintln(w, engineSkips)

	if len(lines) == 0 {
		fmt.Fprintln(w, "escape hatches: none")
		return
	}
	fmt.Fprintln(w, "escape hatches:")
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// insideRepository reports whether root is at or below a git repository. It is
// the discriminator the untracked census needs to tell "I could not ask" from
// "the question has no answer here" — the two silences that used to render
// identically (#292).
//
// ASKED OF THE FILESYSTEM, NEVER OF GIT, and that is the whole point. The case
// being discriminated is precisely the one where git could not be asked: with no
// git on PATH — or with a corrupt index, since `rev-parse --show-toplevel`
// reads no index but every other symptom of a broken repository is git-side —
// using git as the discriminator would answer "not a repository" to a
// repository and collapse the two branches back into the silence this fix
// exists to break.
//
// The walk is upward because root need not be the repository top-level:
// TrackedUnder deliberately runs `git -C root`, which resolves to the nearest
// ancestor repository (#89), so the discriminator has to cover the same ground
// or a repo subdir would be called a non-repository.
//
// A .git DIRECTORY is an ordinary clone; a .git FILE is a linked worktree or a
// submodule, which is a repository too — os.Lstat accepts either, and neither is
// followed. This asks whether the question has an answer here, not whether the
// repository it finds is healthy; a repository too broken to answer is exactly
// the case that earns the line.
func insideRepository(root string) bool {
	dir, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// wouldFireUnder counts, per except.paths glob, how many of the files that glob
// carved out the rule would actually have fired on.
//
// A nil map with a nil error means the question does not apply: the rule's
// verdict is not a per-file one, so evaluating a single carved-out file in
// isolation would invent a number. A whole-tree invariant judges the set it was
// given, and a Finalizer may emit findings no per-file call can produce — for
// either, `0 would fire` would read as "this entry protects nothing" when the
// truth is that nobody asked. Saying so is the same move sql/parses makes for
// unparseable SQL and prefilter-load-bearing makes with `unproven`.
//
// An error is returned rather than swallowed: an unreadable carved-out file
// makes the count wrong in the direction of "this entry is a fossil", which is
// the direction that gets an exemption deleted.
func wouldFireUnder(r *config.Rule, fset *scan.FileSet) (map[string]int, error) {
	if _, isFinalizer := r.Checker.(rules.Finalizer); isFinalizer {
		return nil, nil
	}
	if _, isErrFinalizer := r.Checker.(rules.ErrFinalizer); isErrFinalizer {
		return nil, nil
	}
	// CURRENTLY SHADOWED BY THE TWO ABOVE, and kept deliberately. Measured at
	// this SHA, every rule type declaring WholeTreeInvariant also implements
	// Finalize or FinalizeErr — baseline, methodDelegates, callOrder,
	// patternCount, pairConsistency, required, setRelation — so no mutation of
	// this line changes any output and it is an equivalent mutant, not a gap in
	// the tests. Said plainly because a guard that looks covered and is not is
	// worse than one whose limit is written down.
	//
	// It stays because the shadowing is a property of today's rule set, not of
	// the interface: a whole-tree invariant that answers per file — every file
	// must satisfy P, reported per file — would be neither kind of finalizer,
	// and evaluating one carved-out file in isolation would then invent exactly
	// the number this function exists not to invent.
	if rules.IsWholeTreeInvariant(r.Checker) {
		return nil, nil
	}
	out := map[string]int{}
	for _, f := range fset.Files {
		g, ok := r.CarvedOutBy(f.Path())
		if !ok {
			continue
		}
		ms, err := r.Checker.CheckFile(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Path(), err)
		}
		if len(ms) > 0 {
			out[g]++
		}
	}
	return out, nil
}
