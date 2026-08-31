// formwork-vacuity-census — how many formwork rules currently protect nothing?
//
// Usage: go run -C tools/formwork-vacuity-census . [--full] [--write-inventory] <repo-root>
//
// Product enforcement is formwork type:command with origin on this file.
// Exit 0 = no vacuous rules, 1 = offenders listed or the scope-phase wall
// budget blown (#12419), 2 = usage/env error.
//
// A rule that cannot fail is worse than no rule: it burns CI time and reads as
// coverage in docs/lockdown-enumeration.md while guarding nothing. This census
// classifies every rule in .formwork/rules/ into the three vacuity classes and
// fails CI on any of them, so vacuity cannot silently regrow (#10083).
//
//	class 1  EMPTY SCOPE       scope.include matches no file in the tree.
//	class 1  EMPTY GLOB        ONE scope.include glob matches no file while a
//	                           sibling glob keeps the whole rule looking healthy
//	                           (per-glob arm, #10626 — see below).
//	class 1  GLOB REMOVED      a previously-live include glob was deleted from
//	                           the rule (inventory arm, #10876 — see below).
//	class 1  GLOB UNTRACKED    a live include glob is missing from the inventory.
//	class 1  SOURCE UNLISTED   a rule DECLARES its scope.include exhaustive over a
//	                           source directory (`# source-list-exhaustive:`) and a
//	                           non-test .go file there is covered by no entry
//	                           (#13517 — the file matches no glob, so every arm
//	                           above is blind to it by construction).
//	class 1  SOURCE-LIST-      that declaration names a directory holding no
//	         VACUOUS           non-test Go source, so it gates nothing.
//	class 1  DEAD-EXCEPT-GLOB  an except.paths entry names a literal path that is
//	                           not in the repository (#12178 — see below).
//	class 1  INERT-EXCEPT      an except.paths entry names a literal path that IS
//	                           there and in scope, but which the rule does not
//	                           fire on, so it suppresses nothing (#10777).
//	class 2  SELF-SATISFIED    the rule's evidence is not the thing it guards —
//	                           it is a comment, its own detector, or a token so
//	                           generic that any fragment of the file supplies it.
//	class 2  INERT-REQUIRED    a set-relation whose required side can be blanked
//	                           ENTIRELY with the relation still holding (#12180).
//	class 2  DEAD-TRIGGER      a pair-consistency rule whose trigger matches
//	                           nothing, so no unit ever enters the obligation
//	                           and the companion is never asked for (#12181).
//	class 3  DEAD FIXTURE      the fire/pass pair no longer discriminates.
//	class 3  EMPTY-SCOPE FIRE  no fire fixture contains a file the rule's scope
//	                           matches, so each fires on an EMPTY scope and
//	                           demonstrates nothing about the evidence (#12178).
//	class 3  NO FIRE WITNESS   a heavy rule with no fixture AND no lockdown
//	                           synth that both names it and declares a test
//	                           function (#12178).
//	class 3  FIXTURE REMOVED   a fixture directory the rule used to have is gone
//	                           (registry arm, #10838 — see below).
//	class 3  FIXTURE UNTRACKED a fixture directory is missing from the registry.
//
// # The per-glob arm (#10626)
//
// config.Rule.Applies is a union predicate: once it returns true, which
// include glob earned the match is unrecoverable. Every whole-rule instrument
// inherits that blindness — a rule watching six places, five of which moved
// away, reports a healthy file count while five-sixths of its stated coverage
// is gone. A refactor almost never kills every glob at once, so this is the
// common case of scope rot, and both whole-rule arms (formwork lint's
// empty-scope, class 1 EMPTY-SCOPE) correctly report 0 throughout. The census
// therefore also matches every declared scope.include glob INDIVIDUALLY
// (perglob.go) and gates each dead one as EMPTY-GLOB, naming the glob and how
// many siblings are live. A genuinely aspirational glob is declared in place
// with `# glob-dead: <reason>` on the line above it — the reviewed escape,
// never a separate allowlist file.
//
// scope.exclude and except.paths globs are GATED (#12178), on the half of them
// that discriminates. Two things were wrong with reporting and never gating.
//
// First the DENOMINATOR. The defensive entries were partly "dead" because they
// were measured against the post-skip scan.Walk FileSet rather than against the
// repository: scan.Walk drops .formwork before rules run, so an exclude naming
// '.formwork/**' matched zero SCANNED files while naming thousands of real ones
// — and formwork-engine-skip-declared REQUIRES that declaration on every
// content rule whose include reaches a .formwork path, so the two rules
// demanded opposite things. Subtractive globs are therefore counted against a
// walk that keeps the built-in skip (perglob.go, walkIncludingBuiltinSkip),
// while includes stay on the engine's own FileSet so no coverage claim is ever
// measured over files the engine will not read. Counting a subtraction over a
// superset can only ever be generous.
//
// Second the PREDICATE. "Matches nothing" is the right test for an include and
// the wrong one for an exclude, because the two halves of a scope are not
// symmetric: a dead include OVER-CLAIMS coverage the rule does not have, while
// a dead exclude claims nothing — it is a guard, and an unfired guard is not a
// dead guard. Of the 263 subtractive globs matching nothing here, 232 are
// wildcard CLASS GUARDS (`**/node_modules/**`, `**/*.pbjson.dart`,
// `**/.dart_tool/**`, `.agent-work/**`) naming a class of path that appears
// whenever a build runs or an agent opens a scratch tree; deleting one is a
// regression. The 31 that remain name a LITERAL path, and every one proved to
// be drift — 29 a scripts/check-*.sh gate formwork replaced and deleted, one a
// Dart provider that moved, one a proto that moved. That is the gated half
// (isClassGuard, classify.go).
//
// This measures glob-vs-tree, not glob-vs-intent: a glob that matches the
// WRONG files (too broad, or the right count for the wrong reason) is
// invisible here. That is class 2's territory.
//
// # The inert except.paths entry (#10777)
//
// DEAD-EXCEPT-GLOB above asks whether the path is still THERE. INERT-EXCEPT
// asks the next question — whether an entry whose path IS there still suppresses
// anything — and it is a question only an except.paths entry raises, because
// except.paths is a scope SUBTRACTION rather than a suppression. config.Rule.
// Applies returns false for a matched path, so the rule never evaluates the
// file, no finding is produced to be suppressed, and formwork lint's
// escape-hatch census keeps printing the entry as a live, accounted-for
// exception no matter what happened to the code underneath it. An allowlist
// entry cannot rot this way — lint's exemption-hygiene compares it against real
// findings — which is why the same rot living one level over went unseen.
//
// It was found by accident during #10346: dart-measure-active-step named
// measure_type_filter_providers.dart as the home of a resolver that issue had
// turned into a projection off the server's resolution, and the only thing that
// ever noticed was a human running the rule's own regex across two revisions.
// The cost is governance rather than tidiness — once the list stops saying which
// entries are load-bearing, an honest amendment to an ownership manifest reads
// exactly like an allowlist widening.
//
// Answered by evaluating the rule over a one-file tree with ExceptPaths CLEARED
// (probe.go, exceptExcuses); probing the rule as written would drop the file at
// Applies and report every entry in the corpus inert. Literal paths only, same
// isClassGuard split as the DEAD arm and for the same reason. A file that can
// never be a violator — the declaration home of the very field a rule is written
// against — is declared in place with `# except-declaration: <reason>`, distinct
// vocabulary from `# glob-dead:` because "matches nothing" and "matches a file
// the rule cannot fire on" are different claims with different cures.
//
// # The live-include inventory (#10876)
//
// EMPTY-GLOB only sees declared globs. Deleting a live include glob — and its
// fixtures — leaves no EMPTY-GLOB, no class-3 half-fixture, and a green census:
// the thorough gaming run that dropped admin_app from the global-error-handler
// rules. tools/formwork-vacuity-census/live-include-globs.tsv is the packed
// declared-sibling registry (#12398): every LIVE scope.include glob is listed
// as rule-id<TAB>glob, and removing one from a rule without removing the row
// gates as GLOB-REMOVED. Adding live coverage without a row gates as
// GLOB-UNTRACKED. Regenerate with --write-inventory. Synthetic corpora (rule
// count ≤ minCorpusForCanaries) skip the registry unless they plant a file.
//
// # The fixture-directory registry (#10838)
//
// The class-3 arms judge the fixtures a rule STILL HAS. None of them can see one
// that is GONE: delete a directory from a rule with twelve and `formwork test`
// prints "OK — 11 fixture(s)", `formwork lint` is satisfied by the survivors, and
// this census stays green — so a lockdown is weakenable in two green commits, one
// deleting the fixture that pins a property and one making the change it forbade.
// .formwork/fixtures/<rule-id>/INVENTORY.tsv is the registry that makes the
// deletion leave a trace: a row whose directory is gone gates as
// FIXTURE-REMOVED, a directory with no row as FIXTURE-UNTRACKED. Same shape,
// same canary threshold, the same --write-inventory regeneration and the same
// per-rule keying (#12043) as the live-include registry above — here the rule id
// is the parent directory, so the manifest travels with the tree it registers.
// Details in fixtureinventory.go.
//
// # Why this imports formwork's own matcher
//
// An earlier census expanded each scope.include with the shell and reported 133
// of 200 rules matching zero files — including dart-file-size-cap, which caps
// 3229 live Dart files. formwork scopes are doublestar globs (packages/*/lib/**)
// and `ls` does not expand `**`, so every pattern containing one returned
// nothing. The instrument, not the corpus, was empty. So every scope here is
// matched by config.Rule.Applies and every verdict is rendered by engine.Run —
// the same code the gate engine runs — and the census prints a calibration
// block against known-live rules before it reports a single zero. The per-glob
// arm keeps the same discipline: each glob is counted through a one-glob
// config.Rule (never the shell, never a re-implementation), and the
// calibration block requires dart-file-size-cap's `**/*.dart` glob to report a
// four-figure count and a deliberately-impossible glob to report zero before
// any per-glob zero is believed.
//
// # The scope-phase wall budget (#12419)
//
// Building every rule's in-scope file set is O(rules×files) union-predicate
// work, and the census runs on every Architecture Guardrails pass. Computed
// serially it put minutes on the PR critical path; computed through
// buildScopeIndex (per-rule scans fanned across a GOMAXPROCS pool, each
// rule's own scan serial so file order is preserved) it is a fraction of a
// second on the real corpus. run() builds scopes only through
// buildScopesWithBudget, which refuses the result — exit 1, census FAIL —
// when the phase exceeds scopeWallBudget. The budget value is pinned by the
// formwork rule vacuity-census-wall-budget-pinned, and the wired-through-
// the-index shape by vacuity-census-scope-index-wired: the ceiling is
// mechanical on both axes, never a convention.
//
// # How class 2 is measured
//
// For a rule whose obligation is that something EXISTS (required-pattern
// mode:exists, pattern-count op:at-least), a file that satisfies the rule on its
// own is a WITNESS: it is where the evidence lives. Witnesses are found by
// running the real engine over one-file trees. Three probes then ask whether
// that evidence is the invariant or a shadow of it:
//
//   - COMMENT-SATISFIED — blank every comment-only line of each witness and the
//     rule stops passing. Then prose is the evidence: the code it names could be
//     deleted and the rule would still be green (the #9992 class). A rule that
//     legitimately reads comments declares preprocess: comments-only-*, and is
//     exempt by that declaration rather than by a list.
//   - DETECTOR-SATISFIED — every witness is a gate source (under scripts/ or
//     tools/, or the rule's own declared origin). The rule asserts its own
//     detector still contains some text, not that the detector's invariant holds
//     (the no-zero-reader-columns class). Documented here from the day the
//     census landed and unimplemented until #12178; it is a live gate now.
//   - DIFFUSE-EVIDENCE — split each witness into 16 contiguous blocks; if at
//     least 14 blocks satisfy the rule ALONE, the pattern matches something so
//     common that near-any fragment of the file supplies it.
//
// A ceiling (pattern-count op:at-most, required-pattern every-file) stays
// outside the WITNESS probes: deleting a file satisfies those vacuously, so
// single-file satisfaction does not mean "the evidence lives here" and every
// probe above would misread.
//
// Relations no longer sit outside the population entirely (#12178 defect 1).
// set-relation and pair-consistency were excluded on the same argument, and
// #12180 and #12181 then found live, execution-proven vacuity in exactly those
// two types — 34 set-relation and 45 pair-consistency rules in one shard alone
// had never been probed by anything. They get the two probes that ARE valid for
// a relation, each the mechanical form of the experiment that found the live
// instance:
//
//   - INERT-REQUIRED-SIDE — blank EVERY file the side the relation REQUIRES
//     (params.b.files) draws from, in one experiment, and ask whether the
//     relation still holds. If it does, the whole side could be deleted with
//     the rule staying green: the join is carried by names that appear anyway.
//     Reported only for `subset`/`equal` (where params.b IS the required side)
//     and only where the two sides' globs are separable. Those are the
//     conditions that make the experiment mean anything, not a narrowing —
//     see relation.go, and the coverage line the report prints for the
//     population they exclude, which this instrument does not decide.
//   - DEAD-TRIGGER — no obligation-entering unit matches in scope under the
//     rule's own preprocess and `where` geometry. Entry is trigger, and when
//     also_present is set trigger∧also_present (I17). same-func matches on
//     FuncDecl / var-bound FuncLit spans so multi-line triggers are not false-
//     dead and package-level residue is not false-live (I18). No unit ever
//     enters the obligation, so the `requires` half is never asked for and the
//     rule is green over any tree. This is the pair-consistency spelling of the
//     engine's own AnchorProbe. It is deliberately NOT "duplicate a trigger and
//     see whether the rule notices the extra one": `where: same-file` is this
//     type's DEFAULT and its documented meaning is PRESENCE within a unit, not
//     pairing, so a second trigger beside an existing companion is compliant by
//     definition. Measured against this corpus that probe flagged 93 of 106
//     pair-consistency rules — it was reading the rule type, not the rule.
//   - EMPTY-SIDE — a live equal|subset set-relation whose either extracted side
//     has cardinality 0 on the real tree (V4). empty=empty / empty⊆B holds by
//     set algebra with no load-bearing evidence; the engine's min_count floor
//     closes the same hole when authors set it.
//
// Neither is a witness probe, because a relation is satisfied by an empty tree
// and "this file satisfies it alone" would carry no information.
//
// DIFFUSE-EVIDENCE no longer stops at three witnesses (#12178 defect 3). The
// verdict requires EVERY witness to be internally redundant, so the arm still
// exits on the first witness that pins a line; the old cap excluded exactly the
// rules whose evidence was most redundant. Having MANY witnesses is a separate
// property and stays out — that grain is tools/formwork-witness-census's
// (#12182), and the two must not both claim it.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"

	// Register the built-in rule types, exactly as cmd/formwork does. Without
	// this every rule fails to compile with `unknown type` and the census would
	// report an empty corpus — a false negative of the kind it exists to catch.
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
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
)

const tag = "[formwork-vacuity-census]"

// calibration rules are known-live gates the instrument must report as live
// before any zero it prints is believable. dart-file-size-cap is the named
// canary: the shell census reported it as matching zero files.
var calibration = []struct {
	id     string
	minHit int
}{
	{"dart-file-size-cap", 1000},
	{"no-new-js-ts", 1000},
	{"routes-goroutine-async-ok", 100},
	{"dart-magic-dimensions", 100},
	{"idempotency-not-in-middleware", 1},
}

func main() {
	full := flag.Bool("full", false, "print the whole per-rule classification table, not just offenders")
	writeInv := flag.Bool("write-inventory", false, "rewrite the packed live-include registry (live-include-globs.tsv) from current live include globs and .formwork/fixtures/<rule>/INVENTORY.tsv from the fixture directories on disk — and exit 0")
	inv := flag.String("inventory", "", "path (repo-relative) to the packed live-include registry; default .formwork/census/live-include-globs.tsv")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: formwork-vacuity-census [--full] [--write-inventory] [--inventory PATH] <repo-root>\n")
	}
	flag.Parse()
	if *inv != "" {
		SetInventoryPath(*inv)
	}
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	os.Exit(run(root, *full, *writeInv, os.Stdout, os.Stderr))
}

func run(root string, full, writeInv bool, stdout, stderr io.Writer) int {
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		fmt.Fprintf(stderr, "%s FAIL: repo root %q is not a directory - cannot scan\n", tag, root)
		return 2
	}
	cfg, err := config.Load(root)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: loading .formwork: %v\n", tag, err)
		return 2
	}
	fset, err := scan.Walk(root)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: walking %s: %v\n", tag, root, err)
		return 2
	}
	meta, err := loadRuleMeta(root)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: reading rule metadata: %v\n", tag, err)
		return 2
	}

	// The O(rules×files) scope-membership phase runs through the parallel
	// index under a wall budget (#12419): a serial loop here put the census
	// on the Architecture Guardrails critical path for minutes, and the
	// budget is what keeps that shape from coming back.
	scopes, err := buildScopesWithBudget(cfg.Rules, fset.Files, scopeWallBudget)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: %v\n", tag, err)
		return 1
	}

	gm, err := measureGlobs(root, fset)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: measuring per-glob scopes: %v\n", tag, err)
		return 2
	}

	fixtures, err := onDiskFixtureDirs(root)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: reading fixture directories: %v\n", tag, err)
		return 2
	}

	if writeInv {
		// Both counts say how many files were TOUCHED, not how many exist. A
		// regeneration that reports "wrote 3094" while changing nothing on disk
		// is its own small lie, and it is the lie that hid #13521: the author
		// could not tell a run that cured their finding from one that also
		// rewrote 56 registries belonging to rules they had never seen.
		wroteInc, totalInc, err := writeLiveIncludeInventory(root, gm)
		if err != nil {
			fmt.Fprintf(stderr, "%s FAIL: writing live-include inventory: %v\n", tag, err)
			return 2
		}
		fmt.Fprintf(stdout, "%s live-include registries: %d written, %d unchanged (%d live include globs)\n",
			tag, wroteInc, totalInc-wroteInc, len(liveIncludePairs(gm)))

		wroteFix, totalFix, err := writeFixtureInventory(root, fixtures)
		if err != nil {
			fmt.Fprintf(stderr, "%s FAIL: writing fixture registry: %v\n", tag, err)
			return 2
		}
		fmt.Fprintf(stdout, "%s fixture registries: %d written, %d unchanged (%d fixture directories)\n",
			tag, wroteFix, totalFix-wroteFix, len(fixtures))
		return 0
	}

	// Calibration is written to a buffer rather than straight out, so report()
	// can place it AFTER the verdicts on a failing run (#16031). It is the
	// census's instrument provenance — the evidence that the instrument could
	// have seen something — which is what a PASSING run needs and the least
	// useful text a failing one carries. At 681 bytes it is larger than the
	// whole ~400-byte head that snippet() retains, so while it printed first
	// no failing run could ever name its own offender.
	//
	// A calibration FAILURE flushes immediately: there are no verdicts to lead
	// with, and the calibration lines are the diagnosis.
	var calibrated bytes.Buffer
	if code := calibrate(cfg, scopes, gm, fset, &calibrated, stderr); code != 0 {
		stdout.Write(calibrated.Bytes())
		return code
	}

	inv, invPresent, err := loadLiveIncludeInventory(root)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: reading live-include inventory: %v\n", tag, err)
		return 2
	}
	fixtureInv, fixtureInvPresent, err := loadFixtureInventory(root)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: reading fixture registry: %v\n", tag, err)
		return 2
	}
	// Real guardrail trees must carry both registries; synths may omit them.
	invRequired := len(cfg.Rules) > minCorpusForCanaries

	declared := declaredFuncNames(fset)

	rows, err := classify(cfg, root, fset, scopes, meta, gm, declared)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: %v\n", tag, err)
		return 2
	}
	rows = attachInventoryVerdicts(rows, inventoryVerdicts(gm, inv, invPresent, invRequired))
	rows = attachInventoryVerdicts(rows, fixtureInventoryVerdicts(fixtures, fixtureInv, fixtureInvPresent, invRequired))
	// Unconditional, unlike the two registries above: a source-list declaration
	// is opt-in per rule, so a synth corpus that declares none draws no verdicts
	// without needing a corpus-size gate (#13517).
	rows = attachInventoryVerdicts(rows, sourceListVerdicts(gm, scannedPaths(fset)))

	// The arm one level up (#15837): every arm above gates what it DECIDES, and
	// the two "NOT decided here" counts report() prints are a hole a new rule can
	// still land in. A rule this change ADDS must be one the census can decide.
	// Inert when root is not a git checkout — a proof scratch or a synthetic
	// corpus has no change to judge — and fail-closed when a checkout yields no
	// resolvable range, because an empty added set there is a silent pass for
	// every rule the change introduces (newrules.go).
	added, inCheckout, err := addedRuleIDs(root)
	if err != nil {
		fmt.Fprintf(stderr, "%s FAIL: resolving the rules this change adds: %v\n", tag, err)
		return 2
	}
	// The base side (widening.go). Addition is not the only way into the hole: a
	// rule EDITED out of the decidable set keeps its id on both sides of the diff
	// and is invisible to `added`. Only computed inside a checkout — outside one
	// there is no change to judge and both halves stay inert together.
	var baseReasons map[string]birthFinding
	if inCheckout {
		rangeExpr, err := censusDiffRange(root)
		if err == nil {
			baseReasons, err = baseUndecidedReasons(root, rangeExpr)
		}
		if err != nil {
			fmt.Fprintf(stderr, "%s FAIL: reading the rule corpus this change started from: %v\n", tag, err)
			return 2
		}
		declared, err := existsDeclarations(root)
		if err != nil {
			fmt.Fprintf(stderr, "%s FAIL: reading the exists-multi-file declarations: %v\n", tag, err)
			return 2
		}
		rows = attachInventoryVerdicts(rows, newRuleVerdicts(cfg, meta, declared, added, baseReasons))
	}
	return report(rows, cfg, fset, gm, meta, added, calibrated.Bytes(), inCheckout, full, stdout, stderr)
}

// attachInventoryVerdicts folds inventory findings into the classified rows.
// A verdict whose rule id is not already a row (MISSING-INVENTORY, or a removed
// rule that left inventory orphans) becomes a synthetic row so report() prints it.
func attachInventoryVerdicts(rows []row, byRule map[string][]verdict) []row {
	if len(byRule) == 0 {
		return rows
	}
	index := map[string]int{}
	for i, rw := range rows {
		index[rw.id] = i
	}
	for id, vs := range byRule {
		if i, ok := index[id]; ok {
			rows[i].verdicts = append(rows[i].verdicts, vs...)
			continue
		}
		rows = append(rows, row{id: id, typ: "inventory", verdicts: vs})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	return rows
}
