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
	"github.com/buildfoundry-nz/formwork/internal/vacuity"
)

// armEntry is one fire-* or pass-* directory under a rule's fixture tree.
type armEntry struct {
	name   string
	isFire bool
	// path is the directory the engine EVALUATES, which is the committed
	// fixture tree for a declarative rule and an isolated copy of it for one
	// that spawns a process (#28). Set by isolateArms; never derived again at
	// a call site, so no caller can reach past the isolation.
	path string
	// src is the COMMITTED tree, which is what every message names. An
	// isolated copy lives under a temp directory that is deleted when the run
	// ends, so a manifest error naming it would send the author to a path
	// that no longer exists.
	src string
}

// ArmResult is one fixture arm's outcome.
type ArmResult struct {
	Arm      string   `json:"arm"`  // fixture dir name, e.g. "pass-clean"
	Kind     string   `json:"kind"` // "fire" | "pass"
	Path     string   `json:"path"` // repo-relative fixture dir, slash-separated
	OK       bool     `json:"ok"`
	Problems []string `json:"problems,omitempty"` // the same strings the human output prints
	// Vacuity is set for kind "pass" arms whose base run was OK, and only
	// those: "judged" | "vacuous" | "blind" | "not-judged". vacuous and blind
	// fail the arm; not-judged (a rule outside the verdicts' reach) never does.
	Vacuity       string `json:"vacuity,omitempty"`
	VacuityReason string `json:"vacuity_reason,omitempty"`
}

// RuleResult is one rule's fixture outcome. An orphan fixture dir (#9) is a
// RuleResult with Status "fail", a Reason and no arms.
type RuleResult struct {
	Rule   string      `json:"rule"`
	Status string      `json:"status"`           // "ok" | "fail" | "skip"
	Reason string      `json:"reason,omitempty"` // why skip / orphan
	Arms   []ArmResult `json:"arms"`             // sorted by Arm name; [] never null
}

// Result statuses and arm kinds.
const (
	StatusOK   = "ok"
	StatusFail = "fail"
	StatusSkip = "skip"

	KindFire = "fire"
	KindPass = "pass"
)

const (
	orphanReason     = "dir matches no rule id and can never run"
	noFixturesReason = "no fixtures (formwork lint reports coverage)"
	exemptPrefix     = "declared fixture-exempt: "
)

// judgeVacuity is vacuity.Judge, a variable only so a test can make the probe
// fail: no real rule can, from this package, and the wiring still has to turn
// a probe error into an error rather than a pass.
var judgeVacuity = vacuity.Judge

// newArmResult records one arm's outcome. Problems carry the arm-name prefix
// the human output prints. v is the arm's vacuity verdict: the zero Verdict for
// a fire arm or a pass arm that already failed, otherwise what Judge said. A
// vacuous or blind verdict fails the arm with a problem naming its code.
func newArmResult(ruleID string, a armEntry, ps []string, v vacuity.Verdict) ArmResult {
	kind := KindPass
	if a.isFire {
		kind = KindFire
	}
	ar := ArmResult{
		Arm:  a.name,
		Kind: kind,
		Path: filepath.ToSlash(filepath.Join(".formwork", "fixtures", ruleID, a.name)),
		OK:   len(ps) == 0,
	}
	for _, p := range ps {
		ar.Problems = append(ar.Problems, a.name+": "+p)
	}
	// The !a.isFire half is not pinnable: runFixture never produces a verdict
	// for a fire arm (and neither does the compile-once path), so v is already
	// zero here. It is kept so the "fire arms carry no vacuity" contract holds
	// at the one place ArmResult is built, whatever a caller passes.
	if !a.isFire && v.Kind != "" {
		ar.Vacuity, ar.VacuityReason = v.Kind, v.Reason
		if v.Code != "" {
			ar.OK = false
			ar.Problems = append(ar.Problems, a.name+": "+v.Code+": "+v.Reason)
		}
	}
	return ar
}

// Run evaluates every rule's fixtures under root/.formwork/fixtures and
// writes verdicts to w. It returns the number of failing verdicts (rules
// whose fixtures misbehaved, plus fixture dirs matching no rule id).
// Infrastructure problems (unreadable dirs, bad manifests, un-refreshable
// rules, symlinked fixture trees) return an error instead — never a silent
// pass. The text is rendered from Results, so the two cannot disagree.
func Run(cfg *config.Config, allRuleIDs []string, root string, workers int, w io.Writer) (int, error) {
	results, err := Results(cfg, allRuleIDs, root, workers)
	if err != nil {
		return 0, err
	}
	failed, ran, skipped := 0, 0, 0
	for _, rr := range results {
		switch rr.Status {
		case StatusSkip:
			skipped++
			fmt.Fprintf(w, "[%s] SKIP — %s\n", rr.Rule, rr.Reason)
		case StatusOK:
			ran += len(rr.Arms)
			fmt.Fprintf(w, "[%s] OK — %d fixture(s)%s\n", rr.Rule, len(rr.Arms), notJudgedSuffix(rr))
		default:
			failed++
			if rr.Reason != "" { // orphan: no arms, never ran
				fmt.Fprintf(w, "[%s] FAIL — %s\n", rr.Rule, rr.Reason)
				continue
			}
			ran += len(rr.Arms)
			n := 0
			for _, a := range rr.Arms {
				n += len(a.Problems)
			}
			fmt.Fprintf(w, "[%s] FAIL — %d problem(s)\n", rr.Rule, n)
			for _, a := range rr.Arms {
				for _, p := range a.Problems {
					fmt.Fprintf(w, "  %s\n", p)
				}
			}
		}
	}
	total := len(results) - skipped
	fmt.Fprintf(w, "formwork test: %d/%d rules passed, %d fixture(s) run, %d rule(s) skipped\n",
		total-failed, total, ran, skipped)
	return failed, nil
}

// notJudgedSuffix names the pass fixtures of an OK rule that no vacuity verdict
// could judge, so an OK line never silently covers them. Empty when every pass
// arm was judged.
func notJudgedSuffix(rr RuleResult) string {
	n, reason := 0, ""
	for _, a := range rr.Arms {
		if a.Vacuity == vacuity.KindNotJudged {
			if n == 0 {
				reason = a.VacuityReason
			}
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(", %d pass fixture(s) not vacuity-judged (%s)", n, reason)
}

// Results evaluates every rule's fixtures and returns one RuleResult per rule
// in cfg order (rule-id sorted), then one per orphan fixture dir, sorted.
//
// allRuleIDs is the FULL corpus id set, collected before any --rule scoping
// narrows cfg: the orphan check must know every legitimate fixture-dir name,
// or a scoped run would read sibling rules' fixtures as orphans (#58). A dir
// matching no id in that full set is still a FAIL result; it does not abort
// the run (#9).
func Results(cfg *config.Config, allRuleIDs []string, root string, workers int) ([]RuleResult, error) {
	fixturesRoot := filepath.Join(root, ".formwork", "fixtures")
	results := []RuleResult{}

	// A fixture tree whose directory matches no rule id is unreachable by the
	// per-rule loop below: no run ever opens it, so the proof it holds is dead
	// weight that reads as green. That is still a failure — each orphan is a
	// FAIL verdict, counted in `failed` — but it is not a run abort. Sibling
	// rules still execute. The fail-closed counterpart of the unrecognized-
	// subdir error inside the loop, without the blackout (#9). All orphans
	// are collected here, sorted, and printed after the per-rule loop so a
	// leftover fixture dir cannot stop unrelated proofs.
	known := make(map[string]bool, len(allRuleIDs))
	for _, id := range allRuleIDs {
		known[id] = true
	}
	rootEntries, err := os.ReadDir(fixturesRoot)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("fixtures: reading %s: %w", fixturesRoot, err)
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
			return nil, fmt.Errorf("fixtures: %s is a symlink — refused: the discovery walk does not follow links, so a symlinked fixture tree is either silently skipped or followed by accident; use a real directory",
				filepath.Join(fixturesRoot, e.Name()))
		}
		if !e.IsDir() { // non-dir, non-symlink entries stay ignored (README etc.)
			continue
		}
		if !known[e.Name()] {
			orphans = append(orphans, e.Name())
		}
	}
	sort.Strings(orphans)

	// Read once per run: the except-declaration marker lives only in the rule
	// files' comments, and the census reads the same files.
	inert, err := vacuity.ReadDeclaredInert(root)
	if err != nil {
		return nil, err
	}

	for _, r := range cfg.Rules {
		ruleDir := filepath.Join(fixturesRoot, r.ID)
		entries, err := os.ReadDir(ruleDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("fixtures: reading %s: %w", ruleDir, err)
		}

		var arms []armEntry
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
				return nil, fmt.Errorf("fixtures: %s is a symlink — refused: the fixture walk does not follow links, so a symlinked fixture tree never executes and its rule still reports OK; use a real directory",
					filepath.Join(ruleDir, e.Name()))
			}
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			isFire := strings.HasPrefix(name, "fire-")
			if !isFire && !strings.HasPrefix(name, "pass-") {
				return nil, fmt.Errorf("fixtures: %s: unrecognized fixture dir %q, expected fire-* or pass-*",
					ruleDir, name)
			}
			arms = append(arms, armEntry{name: name, isFire: isFire})
		}

		armTrees, releaseArms, err := isolateArms(r, ruleDir, arms)
		if err != nil {
			return nil, err
		}
		arms = armTrees

		var armResults []ArmResult
		if cmd, ok := ruleCommandCmd(r); ok {
			if shape, ok := parseGoRunShape(cmd); ok && len(arms) > 0 {
				rs, err := runCommandFixturesCompileOnce(r, arms, shape, root, workers)
				if err != nil {
					releaseArms()
					return nil, err
				}
				armResults = rs
			}
		}
		if len(armResults) == 0 {
			for _, a := range arms {
				ps, v, err := runFixture(r, a.path, a.src, a.isFire, root, workers, inert)
				if err != nil {
					releaseArms()
					return nil, err
				}
				armResults = append(armResults, newArmResult(r.ID, a, ps, v))
			}
		}
		releaseArms()
		sort.SliceStable(armResults, func(i, j int) bool { return armResults[i].Arm < armResults[j].Arm })

		if len(armResults) == 0 {
			// The two skips are not the same thing and used to print
			// identically (#53): a declared exemption is a decision, an
			// undeclared one is a rule with no firing proof that nobody
			// decided about.
			//
			// The predicate is the one internal/meta uses for the same field
			// — TrimSpace-non-empty AND heavy — because a rule gets one
			// verdict, not one per command (#336): `fixture_exempt: "   "`
			// records no decision, and fixture_exempt governs HEAVY rules only
			// (docs/reference.md), so a fast rule with a real reason and no
			// fixtures is still the undeclared kind.
			reason := noFixturesReason
			if strings.TrimSpace(r.FixtureExempt) != "" && r.Cost() == rules.CostHeavy {
				reason = exemptPrefix + r.FixtureExempt
			}
			results = append(results, RuleResult{Rule: r.ID, Status: StatusSkip, Reason: reason, Arms: []ArmResult{}})
			continue
		}
		status := StatusOK
		for _, a := range armResults {
			if !a.OK {
				status = StatusFail
			}
		}
		results = append(results, RuleResult{Rule: r.ID, Status: status, Arms: armResults})
	}

	for _, id := range orphans {
		results = append(results, RuleResult{Rule: id, Status: StatusFail, Reason: orphanReason, Arms: []ArmResult{}})
	}
	return results, nil
}

// EvalIn evaluates one rule over the fixture tree rooted at dir, returning its
// findings PRE-suppression together with the walked FileSet. The caller passes
// a rule whose checker is already the one it wants exercised (see runFixture's
// Fresh below); EvalIn owns only what a fixture run must not inherit from the
// repo, so that policy lives in exactly one place rather than once per caller.
//
// Markers stay active — fixtures legitimately exercise marker suppression.
//
// scan.ignore is repo-scoped: its globs are repo-root-relative, but this
// walk's root is the fixture dir, an unrelated path namespace. Applying them
// here would be a namespace collision — a fixture that happens to contain
// vendor/ must still be checked. The same holds for
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
func EvalIn(r *config.Rule, dir, repo string, workers int) ([]finding.Finding, *scan.FileSet, error) {
	fset, err := scan.Walk(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("fixture %s: %w", dir, err)
	}
	// The tree under evaluation is dir; the corpus is repo. Under `check`
	// they are one directory, and a fixture is the plane where they differ —
	// which is what lets {{repo}} reach a detector that lives in the
	// repository while {{root}} stays inside the fixture (#28).
	fset.Repo = repo
	// A fixture plane is exactly where the two differ. Derived rather than
	// passed, so no caller can evaluate a fixture while telling the detector
	// it is judging the live repository.
	fset.Fixture = repo != "" && repo != dir
	findings, err := engine.Run([]*config.Rule{r}, fset, workers)
	if err != nil {
		return nil, nil, fmt.Errorf("fixture %s: %w", dir, err)
	}
	return findings, fset, nil
}

// runFixture evaluates one fixture tree with a fresh checker and returns
// the fixture's problems (empty = fixture behaved as declared) and, for a pass
// arm that behaved, its vacuity verdict over the tree EvalIn already walked.
func runFixture(r *config.Rule, dir, src string, isFire bool, repo string, workers int, inert vacuity.DeclaredInert) ([]string, vacuity.Verdict, error) {
	fresh, err := r.Fresh()
	if err != nil {
		return nil, vacuity.Verdict{}, fmt.Errorf("fixture %s: %w", src, err)
	}
	findings, fset, err := EvalIn(fresh, dir, repo, workers)
	if err != nil {
		return nil, vacuity.Verdict{}, err
	}
	ps, err := judgeFixture(r, dir, src, isFire, findings, fset)
	if err != nil {
		return nil, vacuity.Verdict{}, err
	}
	if isFire || len(ps) > 0 {
		return ps, vacuity.Verdict{}, nil
	}
	v, err := judgeVacuity(r, fset, inert)
	if err != nil {
		return nil, vacuity.Verdict{}, fmt.Errorf("fixture %s: vacuity probe: %w", src, err)
	}
	return ps, v, nil
}

// judgeFixture compares findings against the fixture's declared expectations.
func judgeFixture(r *config.Rule, dir, src string, isFire bool, findings []finding.Finding, fset *scan.FileSet) ([]string, error) {
	findings = finding.Unsuppressed(findings)
	expected, err := collectExpectations(fset, dir, src, r.ID)
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
// ruleShellsOut reports whether this rule's verdict comes from a subprocess.
// type: command does. forbidden-pattern census mode does too: it forwards
// Argv from that same runner, and a bare '-' on its fire manifest accepts a
// detector that failed to compile.
func ruleShellsOut(r *config.Rule) bool {
	if r.Type == "command" {
		return true
	}
	type argvReader interface{ Argv() []string }
	_, ok := r.Checker.(argvReader)
	return ok
}

func commandPinProblems(r *config.Rule, findings []finding.Finding, expected []expectation) []string {
	// Scoped to the rule that shells out, deliberately. A declarative
	// rule's scope-level message ("required pattern not found in any in-scope
	// file") is just as talkative, but it is produced by a checker the engine
	// compiled and the corpus spells those manifests `-` throughout; the
	// newly-reachable hazard is a SUBPROCESS whose failure to run and whose
	// verdict share an exit code.
	if !ruleShellsOut(r) {
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
