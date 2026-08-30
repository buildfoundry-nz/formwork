package fixturetest

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// Run evaluates every rule's fixtures under root/.formwork/fixtures and
// writes verdicts to w. It returns the number of rules with failing
// fixtures; infrastructure problems (unreadable dirs, bad manifests,
// un-refreshable rules, fixture dirs no rule can ever reach) return an error
// instead — never a silent pass.
//
// allRuleIDs is the FULL corpus id set, collected before any --rule scoping
// narrows cfg: the orphan check must know every legitimate fixture-dir name,
// or a scoped run would read sibling rules' fixtures as orphans (#58).
func Run(cfg *config.Config, allRuleIDs []string, root string, workers int, w io.Writer) (int, error) {
	fixturesRoot := filepath.Join(root, ".formwork", "fixtures")
	failed, ran, skipped := 0, 0, 0

	// A fixture tree whose directory matches no rule id is unreachable by the
	// per-rule loop below: no run ever opens it, so the proof it holds is dead
	// weight that reads as green. That is an infrastructure problem (doc
	// comment above: never a silent pass) — the fail-closed counterpart of the
	// unrecognized-subdir error inside the loop. All orphans are reported in
	// one error, sorted, so a run names every dead tree rather than the first.
	known := make(map[string]bool, len(allRuleIDs))
	for _, id := range allRuleIDs {
		known[id] = true
	}
	rootEntries, err := os.ReadDir(fixturesRoot)
	if err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("fixtures: reading %s: %w", fixturesRoot, err)
	}
	var orphans []string
	for _, e := range rootEntries {
		// A symlink is refused loudly regardless of name or target (the #54
		// convention). DirEntry.IsDir is lstat-based, so a symlink-to-dir is
		// "not a dir" here — with an unknown name it would evade both this
		// check and execution (a tree that neither runs nor errors), while a
		// known name would run via os.ReadDir's path-follow below: the same
		// object loud or invisible depending on its name. Refusing makes the
		// policy a decision instead of an lstat accident.
		if e.Type()&fs.ModeSymlink != 0 {
			return 0, fmt.Errorf("fixtures: %s is a symlink — refused: the discovery walk does not follow links, so a symlinked fixture tree is either silently skipped or followed by accident; use a real directory",
				filepath.Join(fixturesRoot, e.Name()))
		}
		if !e.IsDir() { // non-dir, non-symlink entries stay ignored (README etc.)
			continue
		}
		if !known[e.Name()] {
			orphans = append(orphans, e.Name())
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		return 0, fmt.Errorf("fixtures: %d dir(s) match no rule id and can never run: %s (rename to a rule id or delete; a fixture tree that never executes proves nothing)",
			len(orphans), strings.Join(orphans, ", "))
	}

	for _, r := range cfg.Rules {
		ruleDir := filepath.Join(fixturesRoot, r.ID)
		entries, err := os.ReadDir(ruleDir)
		if err != nil && !os.IsNotExist(err) {
			return 0, fmt.Errorf("fixtures: reading %s: %w", ruleDir, err)
		}

		var problems []string
		count := 0
		for _, e := range entries {
			// The same refusal the fixtures-root loop above makes, for the same
			// reason, one level down (#143 row 4). DirEntry.IsDir is lstat-based,
			// so a symlink named fire-1 arrives here with IsDir() false and was
			// skipped by the `!e.IsDir()` line below — the line that exists for
			// `.want` manifests. The fire proof never executed and the run
			// printed `OK — 1 fixture(s)` at exit 0: a skipped proof reading as
			// a pass, aimed at the proof tree itself.
			//
			// Keyed on the type, not the name, to match the root loop and
			// because an unknown-named symlink is the worse case: it would
			// evade the unrecognized-dir error below as well.
			if e.Type()&fs.ModeSymlink != 0 {
				return 0, fmt.Errorf("fixtures: %s is a symlink — refused: the fixture walk does not follow links, so a symlinked fixture tree never executes and its rule still reports OK; use a real directory",
					filepath.Join(ruleDir, e.Name()))
			}
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			isFire := strings.HasPrefix(name, "fire-")
			if !isFire && !strings.HasPrefix(name, "pass-") {
				return 0, fmt.Errorf("fixtures: %s: unrecognized fixture dir %q, expected fire-* or pass-*",
					ruleDir, name)
			}
			count++
			ps, err := runFixture(r, filepath.Join(ruleDir, name), isFire, workers)
			if err != nil {
				return 0, err
			}
			for _, p := range ps {
				problems = append(problems, name+": "+p)
			}
		}

		if count == 0 {
			skipped++
			// The two skips are not the same thing and used to print
			// identically (#53): a declared exemption is a decision, an
			// undeclared one is a rule with no firing proof that nobody
			// decided about. `N rule(s) skipped` blended them, so the ones
			// worth looking at were invisible in the noise.
			//
			// The predicate is the one internal/meta uses for the same field
			// — TrimSpace-non-empty AND heavy — because a rule gets one
			// verdict, not one per command (#336). Read as `!= ""` alone it
			// disagreed with `formwork lint` twice over:
			//
			//   - `fixture_exempt: "   "` printed `declared fixture-exempt:`
			//     with nothing after the colon. Deciding is what the field is
			//     for, and three spaces record no decision, so this rule is
			//     undeclared and belongs in the branch below.
			//
			//   - fixture_exempt governs HEAVY rules only (docs/reference.md).
			//     fixture-coverage judges a fast rule on its fire/pass
			//     fixtures whatever it declares, so a fast rule with a real
			//     reason and no fixtures was skipped here at exit 0 and failed
			//     there — the same rule, the same repository, one command
			//     apart.
			if strings.TrimSpace(r.FixtureExempt) != "" && r.Cost() == rules.CostHeavy {
				fmt.Fprintf(w, "[%s] SKIP — declared fixture-exempt: %s\n", r.ID, r.FixtureExempt)
			} else {
				fmt.Fprintf(w, "[%s] SKIP — no fixtures (formwork lint reports coverage)\n", r.ID)
			}
			continue
		}
		ran += count
		if len(problems) == 0 {
			fmt.Fprintf(w, "[%s] OK — %d fixture(s)\n", r.ID, count)
			continue
		}
		failed++
		fmt.Fprintf(w, "[%s] FAIL — %d problem(s)\n", r.ID, len(problems))
		for _, p := range problems {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}

	total := len(cfg.Rules) - skipped
	fmt.Fprintf(w, "formwork test: %d/%d rules passed, %d fixture(s) run, %d rule(s) skipped\n",
		total-failed, total, ran, skipped)
	return failed, nil
}

// EvalIn evaluates one rule over the fixture tree rooted at dir, returning its
// findings PRE-suppression together with the walked FileSet. The caller passes
// a rule whose checker is already the one it wants exercised (see runFixture's
// Fresh below); EvalIn owns only what a fixture run must not inherit from the
// repo, so that policy lives in exactly one place rather than once per caller.
//
// Allowlists are repo-scoped: their entries are repo-relative paths, but
// fixture trees have their own, unrelated path namespace (fixture-root-
// relative). A colliding path must not be suppressed here. Markers stay
// active — fixtures legitimately exercise marker suppression.
//
// scan.ignore is likewise repo-scoped: its globs are repo-root-relative, but
// this walk's root is the fixture dir. Applying them here would be the same
// namespace collision the allowlist nil exists to prevent — a fixture that
// happens to contain vendor/ must still be checked. The same holds for
// scan.gitignore, and more sharply: the paths a consumer's .gitignore covers
// (build/, .dart_tool/, node_modules/) are exactly the ones fixture trees like
// to use, so a repo-level prune would blind fire fixtures that live there — and
// a fire fixture that stops firing reports as a passing rule.
// Pinned by TestFixtureRunsAreScanIgnoreFree + TestFixtureRunsAreGitignoreFree.
//
// Second consumer: lint's prefilter-load-bearing fixture differential (#133),
// which evaluates a rule and its prefilter-stripped twin over these same trees.
// It needs the identical walk semantics — a repo-level prune there would hide
// exactly the fire fixture that proves a prefilter load-bearing.
func EvalIn(r *config.Rule, dir string, workers int) ([]finding.Finding, *scan.FileSet, error) {
	r = r.CloneWithChecker(r.Checker) // never mutate the caller's rule
	r.Allowlist = nil
	fset, err := scan.Walk(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("fixture %s: %w", dir, err)
	}
	findings, err := engine.Run([]*config.Rule{r}, fset, workers)
	if err != nil {
		return nil, nil, fmt.Errorf("fixture %s: %w", dir, err)
	}
	return findings, fset, nil
}

// runFixture evaluates one fixture tree with a fresh checker and returns
// the fixture's problems (empty = fixture behaved as declared).
func runFixture(r *config.Rule, dir string, isFire bool, workers int) ([]string, error) {
	fresh, err := r.Fresh()
	if err != nil {
		return nil, fmt.Errorf("fixture %s: %w", dir, err)
	}
	findings, fset, err := EvalIn(fresh, dir, workers)
	if err != nil {
		return nil, err
	}
	findings = finding.Unsuppressed(findings)
	expected, err := collectExpectations(fset, dir, r.ID)
	if err != nil {
		return nil, err
	}

	if !isFire {
		var problems []string
		switch {
		case len(expected) > 0:
			problems = append(problems, "pass fixture declares want expectations")
		default:
			if _, err := os.Stat(dir + ".want"); err == nil {
				problems = append(problems, "pass fixture has a .want manifest")
			}
		}
		for _, f := range findings {
			problems = append(problems, "unexpected finding "+expectation{Path: f.Path, Line: f.Line}.String())
		}
		return problems, nil
	}
	if len(expected) == 0 {
		return []string{"fire fixture declares no expectations (add 'want: " + r.ID + "' markers or a .want manifest)"}, nil
	}
	problems := diff(findings, expected)
	if extra := commandPinProblems(r, findings, expected); len(extra) > 0 {
		problems = append(problems, extra...)
		sort.Strings(problems)
	}
	return problems, nil
}

// commandNoOutput matches a command-rule finding message that carries NO tool
// output. internal/rules/command renders exactly two shapes,
//
//	command <argv> exited <n>, want <m>[: <output>]
//	command <argv> output matched forbidden pattern <re>[: <output>]
//
// and its snippet() contributes the ": <output>" tail only when the tool
// printed something (it returns "" for output that is empty after TrimSpace).
// So a message still ENDING at its template's last token is a message whose
// whole text is the frame.
//
// The shape is READ OFF THE MESSAGE rather than reconstructed from the argv,
// which config.Rule does not expose here — and reading it keeps the knowledge
// in one place with internal/rules/command untouched.
//
// It anchors at \z rather than hunting the ": " that introduces output, because
// the argv is interpolated with %v and the shell bodies 110 of the corpus's
// command rules carry are full of ": " of their own; the tail is the only part
// of the message an argv cannot reach. The residual is a tool whose LAST line
// of output happens to end in "exited 1, want 0" — that message reads as silent
// and its bare `-` is accepted. That is the safe direction (a requirement not
// asked for, never a refusal of a correct manifest).
var commandNoOutput = regexp.MustCompile(
	`(?:exited -?\d+, want -?\d+|output matched forbidden pattern "(?:[^"\\]|\\.)*")\z`)

// commandOutputTail is commandNoOutput's positive twin: the same two template
// tails followed by the ": " that introduces output. The LAST match is the real
// frame boundary — an argv can spell the template's own words, and only the
// rightmost occurrence can be where the frame ends.
var commandOutputTail = regexp.MustCompile(
	`(?:exited -?\d+, want -?\d+|output matched forbidden pattern "(?:[^"\\]|\\.)*"): `)

// commandPinProblems refuses a command rule's fire manifest that leans on a
// bare `-` where the tool actually PRINTED something (#262 finding 4).
//
// WHAT A BARE `-` IS WORTH HERE. It is an expectation with an empty MessagePin,
// and diff ignores Message for those, so any scope-level finding satisfies it.
// command.FinalizeErr emits exactly one scope-level Match whatever happened, so
// such a manifest holds only "the tool disagreed with expect:" — including the
// case where the disagreement is a checker that never COMPILED and the message
// is a go build error. That hazard is new to the Go ports: the sh/py originals
// had no compile step, so a broken detector could not previously wear the same
// exit code as a firing one.
//
// WHY OUTPUT-CONDITIONAL AND NOT ABSOLUTE. Ten command rules in
// examples/palletra-port-full exit non-zero on their fire path while printing
// nothing at all (measured by evaluating each in its own fire-1 tree; they are
// exactly the ten whose fire manifests are still bare). Their message is the
// frame alone, so no substring of it can name a verdict and there is no pin to
// write. Conditioning on output needs no exemption list for them AND still
// closes the compile hazard on those same ten, because a checker that fails to
// build prints — where a standing opt-out would let them accept build errors
// forever.
//
// TWO QUESTIONS, IN THIS ORDER, because each guards the next from over-reach:
// did the tool print, and only then, does the manifest lean on message-
// blindness? A fixture whose rule produced no finding, or produced a silent
// one, is left entirely to diff — which already reports the manifests that are
// wrong for those reasons, and would be double-reported by a requirement that
// asked the second question first.
func commandPinProblems(r *config.Rule, findings []finding.Finding, expected []expectation) []string {
	// Scoped to the rule type that shells out, deliberately. A declarative
	// rule's scope-level message ("required pattern not found in any in-scope
	// file") is just as talkative, but it is produced by a checker the engine
	// compiled and the corpus spells those manifests `-` throughout; the
	// newly-reachable hazard is a SUBPROCESS whose failure to run and whose
	// verdict share an exit code.
	if r.Type != "command" {
		return nil
	}
	printed := ""
	for _, f := range findings {
		if !commandNoOutput.MatchString(f.Message) {
			printed = f.Message
			break
		}
	}
	if printed == "" {
		return nil // nothing was printed, so no manifest here can be leaning on it
	}
	for _, e := range expected {
		// Scope-level and unpinned is the whole defect. A line-anchored
		// expectation with no pin is not it: it names a location, and for a
		// command rule diff will report it unmatched on its own.
		if e.Path != "" || e.Line != 0 || e.MessagePin != "" {
			continue
		}
		return []string{fmt.Sprintf("fire fixture declares a bare '-' for a command rule whose tool printed output: a bare '-' is satisfied by ANY scope-level finding, so it accepts a checker that merely failed to COMPILE as readily as the verdict this fixture is meant to prove. Pin a substring of what the tool printed (`- <substring>`); it said: %s",
			oneLine(commandOutput(printed)))}
	}
	return nil
}

// commandOutput returns just the tool's output from a command finding message,
// so the refusal above quotes what an author has to pin rather than the argv
// they already wrote. Falls back to the whole message when the frame cannot be
// located, which is the honest answer for a shape this package does not know.
func commandOutput(msg string) string {
	m := commandOutputTail.FindAllStringIndex(msg, -1)
	if len(m) == 0 {
		return msg
	}
	return msg[m[len(m)-1][1]:]
}

// oneLine renders tool output as a single bounded line: Run prints one problem
// per line, and the output being quoted is routinely multi-line (a go build
// error is at least two). Detector names and build errors land early, so the
// head is what is kept.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 200
	if len(s) <= max {
		return s
	}
	// Walk back to a rune boundary: the quoted text is whatever the tool
	// printed, and a message split mid-rune reaches report.GitHub's annotations.
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
