// Package command implements the `command` rule type (spec §5): the interim
// escape hatch for toolchain gates that cannot be expressed declaratively. It
// runs an external command at the repository root and interprets its exit code
// (and optionally its output). It is a heavy, whole-run rule: a failure to
// execute the tool is an engine error (exit 2), never a silent pass (spec §11).
// Every command rule is enumerated by `formwork lint` (spec §9).
//
// The tool runs with the caller's environment INHERITED UNCHANGED — that is the
// escape hatch's contract, and this package removes nothing from it. It refuses
// two states instead, both in ensureRepositoryAgreement, and refusing is what
// lets the contract stand: nothing is deleted from the tool's environment.
//
//   - an ambient POINTER variable (GIT_DIR, GIT_WORK_TREE, GIT_COMMON_DIR)
//     naming a repository other than the one the engine is checking — the one
//     state an identity comparison can SEE (#177);
//   - any of the OBJECT-STORE family being set at all, which moves which
//     commits and objects git answers from INSIDE the right repository and so
//     is invisible to that comparison (#213).
//
// Both are exit 2, never a finding, and both are lifted by FORMWORK_GIT_ENV=
// inherit — the same hatch internal/vcs uses, so an operator who has already
// taken that decision does not meet a second refusal spelled differently.
package command

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
	"gopkg.in/yaml.v3"
)

type commandParams struct {
	Cmd    []string   `yaml:"cmd"`
	When   *whenSpec  `yaml:"when"`
	Expect expectSpec `yaml:"expect"`
}

type whenSpec struct {
	PathsChanged []string `yaml:"paths_changed"`
}

type expectSpec struct {
	Exit         *int   `yaml:"exit"`          // expected exit code; default 0
	OutputForbid string `yaml:"output_forbid"` // optional: a violation if this regex matches output
}

type command struct {
	cmd          []string
	whenGlobs    []string // non-nil ⇒ run only if a scanned file matched one
	expectExit   int
	outputForbid *regexp.Regexp

	sawTrigger atomic.Bool
	// skipped records that FinalizeErr took the when: early return. It is a
	// record of what happened, not a re-derivation from whenGlobs/sawTrigger:
	// those two also read "would skip" before the finalizer has run at all, and
	// a disclosure that describes a decision not yet taken is a different claim
	// from one that reports it.
	skipped atomic.Bool
}

func newCommand(params *yaml.Node) (rules.Checker, error) {
	var p commandParams
	if err := rules.DecodeParams(params, &p); err != nil {
		return nil, err
	}
	if len(p.Cmd) == 0 {
		return nil, errors.New("command: params.cmd must be a non-empty argv list")
	}
	c := &command{cmd: p.Cmd, expectExit: 0}
	if p.Expect.Exit != nil {
		c.expectExit = *p.Expect.Exit
	}
	if p.When != nil {
		if len(p.When.PathsChanged) == 0 {
			return nil, errors.New("command: when.paths_changed must be a non-empty glob list when when: is set")
		}
		for _, g := range p.When.PathsChanged {
			if !doublestar.ValidatePattern(g) {
				return nil, fmt.Errorf("command: invalid when.paths_changed glob %q", g)
			}
		}
		c.whenGlobs = p.When.PathsChanged
	}
	if p.Expect.OutputForbid != "" {
		re, err := regexp.Compile(p.Expect.OutputForbid)
		if err != nil {
			return nil, fmt.Errorf("command: invalid expect.output_forbid: %w", err)
		}
		c.outputForbid = re
	}
	return c, nil
}

// Cost marks command rules heavy (spec §8): they shell out and belong to
// heavier lanes, not the fast per-commit path. --skip-escapes and
// fixture-exemption key on this, not on ProcessBound.
func (*command) Cost() rules.Cost { return rules.CostHeavy }

// analyzerAtCommandPosition matches a Dart/Flutter analyzer invocation inside a
// shell script body: the word at a command position (start, after a newline, or
// after ; & | && || or an opening paren/backtick/$( ) followed by a subcommand
// word that ends at whitespace, a newline, or end of the argument.
//
// The trailing `[a-z][a-z0-9-]*(?:[ \t]|$)` is what keeps this off the two
// non-invocations measured in the corpora — a python `dart = 1` assignment (the
// next token is `=`, not a word) and an `echo flutter step: build` label (the
// next token carries a colon, so it does not end at whitespace). Both are
// pinned by TestProcessBoundDoesNotBindANonInvocation.
var analyzerAtCommandPosition = regexp.MustCompile(
	"(?:^|[\\n;&|(`]|\\$\\()[ \\t]*(?:dart|flutter)[ \\t]+[a-z][a-z0-9-]*(?:[ \\t\\n]|$)")

// analyzerBinary reports whether an argv element NAMES the analyzer binary, by
// basename so an absolute or toolchain-relative path still counts.
func analyzerBinary(arg string) bool {
	switch filepath.Base(arg) {
	case "dart", "flutter":
		return true
	}
	return false
}

// launchers run the command in their remaining arguments, so an analyzer named
// there is still an analyzer this rule spawns. fvm is the common one in Flutter
// repositories; the rest are the ordinary process wrappers.
var launchers = map[string]bool{
	"fvm": true, "xargs": true, "env": true, "nice": true, "ionice": true,
	"timeout": true, "nohup": true, "stdbuf": true, "command": true,
}

// shells take a script body after -c, which is the spelling 110 of the 135
// command rules in examples/palletra-port-full use.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true,
}

// ProcessBound reports whether this rule's subprocess is analyzer-class — the
// multi-GB Dart/Flutter footprint the machine-wide gate exists to bound (#67,
// #81). True answers go to the width-capped pool; false answers run at full
// --workers.
//
// IT MATCHES SPELLINGS, NOT argv0, and that is #81's residual. This used to
// compare argv0 against the literal strings "dart" and "flutter". Measured
// against examples/palletra-port-full that bound 0 of 135 command rules, and
// against the validating target at its pinned SHA 2 of 86 — because 110 of
// those 135 are `bash -c '...'` and 84 of those 86 are `go run`, so the
// analyzer, where there is one, is reached THROUGH a wrapper. Five of six real
// spellings (absolute path, fvm, three shell wrappers) read as
// not-an-analyzer and ran at full width, which is the failure #81 describes.
//
// WHERE IT LOOKS IS THE PRECISION, and it is deliberately not "every argument".
// Command rules routinely carry pattern TABLES as arguments — the measured case
// is a rule whose data includes the line `dart run melos exec~melos affected-
// test runner`, which is textually identical to an invocation. Scanning every
// argument makes every such table a false positive, so only three positions are
// read: argv0, a launcher's remaining arguments, and a shell's -c body. A
// python3 rule carrying `dart = 1` is therefore never scanned at all.
//
// THE SHELL-BODY ARM STILL OVER-APPROXIMATES, and that is the deliberate
// direction. Inside a -c body a data line and a command are indistinguishable,
// so the one corpus rule above is bound though it only greps for those strings.
// The asymmetry decides it: a false positive serialises one rule through the
// K=1 pool, while a false negative is five sessions forking 8.6 GB analyzers —
// the machine hang #81 was filed about.
//
// WHAT IS NOT DECIDED BY ARGV, and the decision taken about it (#236, measured
// and closed). A wrapper naming no analyzer — `make analyze`, or a script path
// whose contents formwork does not read — is undecidable from argv and answers
// false. That is a fail-open residue, and rules.ProcessBoundOf's own default
// argues for closing it by binding every CostHeavy command unless declared
// otherwise.
//
// IT IS DELIBERATELY LEFT OPEN, on two measurements rather than on preference:
//
//   - Cost. `formwork check` over examples/palletra-port-full (135 command
//     rules), 6 samples each: 1.81s median with argv detection, 2.72s with
//     ProcessBound forced true for every command rule — +50%, with no overlap
//     between the two ranges (max 1.92 < min 2.67). Every non-analyzer rule
//     would serialise through the K=1 pool to buy it.
//   - Exposure. Across both corpora, 221 command rules, the argv0 histogram is
//     bash 110, go 84, python3 21, sh 4, dart 2. ZERO carry an argv0 this
//     function cannot classify — no `make`, no bare script path. The residue
//     the flip would close is empty in practice.
//
// So the flip costs 50% of every run to close a case that does not currently
// occur. If one ever does — a `make` or a bare script that reaches an analyzer —
// the cheap answer is to name it here, not to bind everything. What would change
// the decision is a corpus where that histogram grows a category this function
// cannot read.
func (c *command) ProcessBound() bool {
	if len(c.cmd) == 0 {
		return false
	}
	argv0 := filepath.Base(c.cmd[0])
	if analyzerBinary(c.cmd[0]) {
		return true
	}
	if launchers[argv0] {
		for _, a := range c.cmd[1:] {
			if analyzerBinary(a) {
				return true
			}
		}
	}
	if shells[argv0] {
		for i, a := range c.cmd[1:] {
			if a != "-c" || i+2 >= len(c.cmd) {
				continue
			}
			if analyzerAtCommandPosition.MatchString(c.cmd[i+2]) {
				return true
			}
		}
	}
	return false
}

// CheckFile records whether a file THIS RULE WAS GIVEN matches the
// when.paths_changed trigger. The engine calls it only for files passing the
// rule's own scope, so sawTrigger cannot speak for the whole scan — a rule whose
// scope excludes its trigger paths never sees them, however they were scanned.
// Within that scope, and in --staged/--range modes where the scanned set is the
// changeset, it answers "did a trigger path change?". It never emits per-file
// findings.
func (c *command) CheckFile(f *scan.File) ([]rules.Match, error) {
	if len(c.whenGlobs) == 0 {
		return nil, nil
	}
	p := f.Path()
	for _, g := range c.whenGlobs {
		if ok, _ := doublestar.Match(g, p); ok {
			c.sawTrigger.Store(true)
			return nil, nil
		}
	}
	return nil, nil
}

// FinalizeErr runs the command once, at the repo root. A when: rule is skipped
// when no file it was given matched its trigger (see CheckFile — that is not the
// same as "no such file was scanned"). A tool that cannot be executed is an
// error (exit 2); a wrong exit code or forbidden output is a finding.
//
// The repository-agreement guard sits AFTER the when: return and before the
// exec, which is the altitude the question belongs at: a rule whose tool does
// not run has no environment to disagree about, and refusing there would fail
// runs over a gate that was never going to execute.
func (c *command) FinalizeErr(ctx rules.FinalizeContext) ([]rules.Match, error) {
	if len(c.whenGlobs) > 0 && !c.sawTrigger.Load() {
		// Correct, and it must not be silent: with the tool unrun this rule
		// renders `[id] OK` like one that ran and passed (#159). SkipReason is
		// what the reporting surface reads back.
		c.skipped.Store(true)
		return nil, nil
	}
	// Cleared on the running path, so the flag describes THIS finalize rather
	// than the last one that skipped. Nothing re-finalizes a checker instance
	// today; a sticky true would make the first caller that does report a skip
	// for a run whose tool executed.
	c.skipped.Store(false)
	if err := ensureRepositoryAgreement(ctx.Root); err != nil {
		return nil, fmt.Errorf("command %v: %w", c.cmd, err)
	}
	cmd := exec.Command(c.cmd[0], c.cmd[1:]...)
	cmd.Dir = ctx.Root
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			return nil, fmt.Errorf("command %v: %w", c.cmd, err)
		}
	}
	if exit != c.expectExit {
		return []rules.Match{{Message: fmt.Sprintf("command %v exited %d, want %d%s", c.cmd, exit, c.expectExit, snippet(out))}}, nil
	}
	if c.outputForbid != nil && c.outputForbid.Match(out) {
		return []rules.Match{{Message: fmt.Sprintf("command %v output matched forbidden pattern %q%s", c.cmd, c.outputForbid.String(), snippet(out))}}, nil
	}
	return nil, nil
}

// ensureRepositoryAgreement refuses to run the operator's tool when the ambient
// git environment resolves a repository other than the one root names — the
// state where a single `check` invocation answers for TWO repositories, the
// declarative rules for the repository `-C` names and this rule's tool for
// whatever the POINTER FAMILY — GIT_DIR, GIT_WORK_TREE, GIT_COMMON_DIR — points
// at, with nothing in the output to tell them apart (#177). Measured, not
// argued: with this guard removed,
// TestCommandRefusesWhenAmbientGitDirNamesAnotherRepository runs the tool and it
// answers the repository GIT_DIR named rather than root's, and a binary built
// from that same state ran `git rev-parse --absolute-git-dir` against the OTHER
// repository's git directory and then reported THIS rule's verdict over that
// answer. What the run's exit code is depends on the rule's expect:, which is
// the point — with the default expect the two-repository run is a SILENT exit 0,
// `[which-repo] OK`, 1/1 rules passed. The guarded binary exits 2 on the same
// invocation, naming both git directories and both work trees.
//
// IT REFUSES RATHER THAN SCRUBBING, which is deliberately NOT what internal/vcs
// does for its own git calls, and the difference is whose command it is. There,
// formwork wrote the argv and named the repository with `-C`, so an ambient
// pointer is a redirection the caller cannot have meant and removing it restores
// the question that was asked. Here the argv is the OPERATOR's and formwork
// does not know what the tool reads: `command` is the disclosed escape hatch —
// tools need PATH, HOME and whatever else they were configured with — so
// deleting variables from an arbitrary tool's environment would be formwork
// deciding on that tool's behalf that they were not meant. Refusing changes what
// no existing command rule sees, because nothing is removed; it changes only the
// state that was already broken, turning a silent answer about the wrong
// repository into an engine error (exit 2, spec §11).
//
// A SCRUB WOULD NOT HAVE BEEN SUFFICIENT EITHER. internal/vcs/env.go records the
// measurement: with root inside another repository, removing GIT_DIR does not
// remove the wrong answer, it substitutes the ANCESTOR's — which is why the
// scrub there is paired with a refusal on exactly this comparison, and why
// asking for that verdict gets the guard rather than half of it.
//
// IT IS NOT NARROWED TO RULES THAT INVOKE GIT, and cannot be. A command rule's
// argv[0] is as often `sh`, `make` or a project script as it is `git`, and what
// those run is not inspectable from here — so the question is asked of the run's
// ambient pointer variables for every command rule whatever its argv, rather
// than of this rule's tool. It is still only that one question, which is what
// the residual paragraph below is about.
//
// THE REFUSAL HALF OF THE POLICY IS ASKED FOR, NOT COPIED — and it is only the
// refusal half. The pointer list, the FORMWORK_GIT_ENV hatch and the
// refuse-on-effect comparison all live in internal/vcs; a second copy here would
// be free to drift from the one every other git-touching part of the engine
// obeys. internal/vcs's OTHER half does not carry over: it scrubs those
// variables out of its own git calls, and this package scrubs nothing, on
// purpose (above). So what this rule type shares with the rest of the engine is
// the same refusal on the same comparison — not the same environment.
//
// vcs.CommonDir is a question about which repository root resolves to, and
// internal/vcs applies the environment policy before running git — so its
// refusal, which is the tagged ErrGitEnv, is available whether or not git can
// then answer. Every other error it returns belongs to git, not to
// this policy, and is discarded here: a command rule over a root that is not a
// repository at all must keep running, which is every `formwork test` fixture
// tree.
//
// THE HALF AN IDENTITY QUESTION CANNOT SEE (#213, closed by the call above).
// vcs.CommonDir asks which repository root resolves to, so the only variables it
// can see are the ones that move that identity. The OBJECT-STORE family
// (GIT_ALTERNATE_OBJECT_DIRECTORIES, GIT_GRAFT_FILE, GIT_NO_REPLACE_OBJECTS,
// GIT_OBJECT_DIRECTORY, GIT_REPLACE_REF_BASE, GIT_SHALLOW_FILE) moves which
// commits and objects git answers from INSIDE the repository root names — the
// git directory, the shared directory and the work tree stay byte-identical, so
// there is nothing for a comparison to disagree about. internal/vcs removes
// those six from its OWN git calls (#176); here they were inherited and
// unrefused.
//
// Measured on this tree before the fix, not predicted: a rule
// `cmd: [git, cat-file, -e, SHA]` for a SHA present only in ANOTHER repository
// FAILED at exit 1 with a clean environment and PASSED at exit 0, `[hist] OK`,
// under an ambient GIT_ALTERNATE_OBJECT_DIRECTORIES naming that repository's
// object store — a silent green over objects nobody named.
//
// REFUSED, NOT SCRUBBED, which is the decision #213 asked for. Deleting the six
// from cmd.Env would be formwork deciding on the operator's tool's behalf and
// would change what an already-working rule sees; refusing changes no tool's
// environment and cannot be mistaken for a finding. Presence is the trigger
// because there is nothing to compare against, and that is affordable only
// because git does not set these where formwork runs (measured in
// internal/vcs/env.go). Where an operator does mean one — receive-pack
// quarantine is the real case — the hatch is the answer.
//
// COST: one extra `git rev-parse` per command rule that actually runs — ~10ms
// measured on git 2.50.1 (macOS), against a rule type that is CostHeavy because
// it forks a whole toolchain. Where one of the pointer variables IS set,
// internal/vcs adds its own two identity forks on top; that is the run this
// exists for.
func ensureRepositoryAgreement(root string) error {
	// The object-store family first (#213), because it is the half the identity
	// question below is structurally blind to: those six move which commits and
	// objects git answers from INSIDE the repository root names, so CommonDir
	// returns the right answer and the tool is still answered from history
	// nobody named. Presence is the trigger; vcs.EnsureNoInheritedHistoryEnv
	// owns the list, the hatch and the reasoning, so this package does not
	// restate a list that would drift from the one #176 scrubs.
	if err := vcs.EnsureNoInheritedHistoryEnv(); err != nil {
		return err
	}
	_, err := vcs.CommonDir(root)
	if errors.Is(err, vcs.ErrGitEnv) {
		return err
	}
	return nil
}

// SkipReason implements rules.SkipReporter: it reports the when: skip that
// FinalizeErr took, and only after it was taken. The sentence names the gate,
// the globs nothing matched, and the argv that did not run — the three facts an
// operator needs to tell this apart from a gate that ran clean.
func (c *command) SkipReason() (string, bool) {
	if !c.skipped.Load() {
		return "", false
	}
	// "no file in this rule's scope", not "no scanned file": CheckFile is only
	// handed files that pass the rule's own scope, so sawTrigger cannot speak
	// for the scan. The wider claim contradicted the summary line above it
	// whenever a trigger path was scanned but excluded from this rule — and it
	// sent the reader to inspect their changeset when the defect was in scope:.
	return fmt.Sprintf("skipped: no file in this rule's scope matched when.paths_changed (%s), so %v did not run",
		strings.Join(c.whenGlobs, ", "), c.cmd), true
}

// TriggerGlobs returns the when.paths_changed globs that gate this rule's
// whole-run execution, or nil when it declares no when: gate. It answers the
// STATIC question — what the gate IS — where SkipReason above answers the
// per-run one, and only once FinalizeErr has actually declined.
//
// Both are needed and neither substitutes for the other. `formwork lint` never
// executes a heavy rule, so it never reaches a skip to report; the configuration
// is all it has to judge, and #161 is a gate whose scope and trigger cannot
// intersect — dead on every commit, in every mode, which no single run's skip
// disclosure can tell apart from a run that simply changed nothing relevant.
//
// The copy is deliberate: whenGlobs is read-only after construction and shared
// by every goroutine evaluating this rule, so handing out the live slice would
// let a diagnostic caller's mutation become a data race in the checker.
func (c *command) TriggerGlobs() []string {
	if len(c.whenGlobs) == 0 {
		return nil
	}
	return append([]string(nil), c.whenGlobs...)
}

// snippet renders a short, single-line-ish extract of tool output for a finding
// message, or "" when there was none. When the output exceeds the byte budget
// both ends are kept: detectors print per-finding lines first (the names
// authors and lockdown synth tests need) and the actionable summary last
// (the cure CI annotations must teach). Each cut is walked to a rune boundary
// so the result is always valid UTF-8 — required because report.GitHub emits
// f.Message into a ::error:: annotation that must not contain split multi-byte
// runes.
func snippet(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	// 800 bytes keeps the first ~400 of detector output (finding names land
	// early; the original pure-head budget was 400) plus a ~400-byte cure tail.
	// Pure-head cut loses the cure; pure-tail cut loses finding names
	// (lockdown-synth fallout on the asadmin / supplier-collaboration gates).
	const max = 800
	if len(s) <= max {
		return ": " + s
	}
	// headBudget + len(mid) + tailBudget == max.
	const mid = "…"
	const headBudget = 400
	tailBudget := max - headBudget - len(mid)

	headEnd := headBudget
	// Walk back so we never end mid-rune.
	for headEnd > 0 && !utf8.RuneStart(s[headEnd]) {
		headEnd--
	}
	head := s[:headEnd]

	tailStart := len(s) - tailBudget
	if tailStart < headEnd {
		tailStart = headEnd
	}
	// Walk forward so we never start mid-rune.
	for tailStart < len(s) && !utf8.RuneStart(s[tailStart]) {
		tailStart++
	}
	tail := s[tailStart:]

	return ": " + head + mid + tail
}

func init() {
	rules.Register("command", newCommand)
}
