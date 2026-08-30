// gate_test.go — `make gate` cannot be misread: its verdict is the LAST line
// it prints, and its exit status is make's own rather than a pipeline's.
// Converted from scripts/gate-proof.sh.
//
// WHY THIS EXISTS. `make verify 2>&1 | tail -40` reports TAIL's exit status.
// In one session that produced four consecutive "verify is green" claims while
// the failures sat above the window. `make gate` is the answer: a verdict that
// survives a pipe, because the last line says PASS or FAIL in words and the
// status is make's.
//
// The proof drives the gate through GATE_CMD against a suite that deliberately
// fails, rather than running the real `make verify` twice — which would put
// minutes into `make verify` itself. GATE_CMD exists for this and must never be
// used to narrow a real gate run; the announcement arm below is what keeps that
// honest.
package repoproof_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runGate runs `make gate` with an inner command, returning exit code, stdout
// and the log path it reported.
func runGate(t *testing.T, inner string, extra ...string) (int, string) {
	t.Helper()
	args := append([]string{"gate"}, extra...)
	if inner != "" {
		args = append(args, "GATE_CMD="+inner)
	}
	cmd := exec.Command("make", args...)
	cmd.Dir = repoRoot(t)
	// STDOUT AND STDERR ARE KEPT APART, deliberately. The verdict is the last
	// line of STDOUT; make's own `*** [gate] Error N` goes to stderr and would
	// otherwise land after it. Merging them is what a careless reader does, and
	// the arm below pins what that reader sees instead.
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("make gate could not run: %v", err)
	}
	return code, stdout.String()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestGatePassingSuiteEndsInThePassVerdict(t *testing.T) {
	needBinary(t, "make")
	code, out := runGate(t, "true")
	if code != 0 {
		t.Fatalf("a passing inner command must exit 0, got %d:\n%s", code, out)
	}
	if got := lastLine(out); got != "GATE: PASS" {
		t.Fatalf("the verdict must be the LAST line — that is what survives a pipe; got %q", got)
	}
}

// The inner status is carried in the verdict TEXT, over several inner exits.
//
// WHY THE TEXT AND NOT THE STATUS. make flattens every recipe failure to its
// own exit 2, so this repo's 0/1/2 contract (pass / violations / engine or
// config error) does not survive the make boundary at all; the verdict line is
// the ONLY place the 1-vs-2 distinction still lives. The deleted shell proof
// looped inner exits 1 and 2 asserting the exact string `GATE: FAIL (exit N)`.
//
// The Go port replaced that with `strings.Contains(out, "3")` over a single
// vector, which is satisfied by a digit anywhere in the output — the printed
// temp-log path on this machine is /var/folders/zt/053vjfd5.../T, so the
// assertion held over a verdict flattened to a hardcoded `(exit 1)` (#300).
// Asserted on the verdict LINE, matched whole, so no other output can satisfy
// it.
func TestGateFailingSuiteSurvivesThePipe(t *testing.T) {
	needBinary(t, "make")
	for _, want := range []int{1, 2, 3} {
		code, out := runGate(t, fmt.Sprintf("sh -c 'echo inner-output; exit %d'", want))
		if code == 0 {
			t.Fatalf("a failing inner command must NOT exit 0:\n%s", out)
		}
		verdict := lastLine(out)
		if verdict != fmt.Sprintf("GATE: FAIL (exit %d)", want) {
			t.Errorf("the verdict must be the LAST line of stdout and must NAME the inner "+
				"status — make flattens the recipe to exit 2, so the text is the only place "+
				"the 1-vs-2 contract survives; got %q for an inner exit %d:\n%s", verdict, want, out)
		}
		// The verdict must not be readable as a pass anywhere near it.
		tailWindow := out
		if len(out) > 400 {
			tailWindow = out[len(out)-400:]
		}
		if strings.Contains(tailWindow, "GATE: PASS") {
			t.Errorf("a failing run printed a PASS verdict near the end:\n%s", tailWindow)
		}
		// The captured run is re-emitted VERBATIM, and the bounded failure lift
		// sits below it. Both are separate promises of the recipe and the port
		// asserted neither: `strings.Contains(out, "inner-output")` was satisfied
		// by the lift's own `GATE: | inner-output` line, so deleting the
		// recipe's `cat "$log"` — the re-emission of the whole captured run —
		// left this test green while an operator lost every line above the last
		// twenty. Matched as a WHOLE line, which the prefixed lift cannot be.
		if !hasExactLine(out, "inner-output") {
			t.Errorf("the captured run must be re-emitted verbatim, not merely quoted in the "+
				"failure lift — the gate captures INSIDE the recipe precisely so the operator "+
				"still sees the suite's own output:\n%s", out)
		}
		if !strings.Contains(out, "GATE: ---- failure context ----") {
			t.Errorf("a failing gate printed no failure-context block, so a short tail is a "+
				"prompt to go hunting rather than an answer:\n%s", out)
		}
		if !strings.Contains(out, "GATE: | inner-output") {
			t.Errorf("the failure-context block must lift the tail of the log next to the "+
				"verdict:\n%s", out)
		}
		// And the log path is printed AND the log is really there. The comment
		// this replaces claimed the path was asserted; deleting the
		// `echo "GATE: full log: $log"` line left the port green (#300).
		path := logPathFrom(out)
		if path == "" {
			t.Fatalf("a failing gate printed no `GATE: full log:` line, so there is nothing "+
				"to go read:\n%s", out)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("the printed log path does not exist — the line is decoration: %v", err)
			continue
		}
		if !strings.Contains(string(data), "inner-output") {
			t.Errorf("the kept log does not hold the inner output, got %q", string(data))
		}
		os.Remove(path)
	}
}

// hasExactLine reports whether out carries want as a WHOLE line. Substring
// matching cannot tell the gate's verbatim re-emission of the log from the
// failure lift's `GATE: | ` quotation of the same text.
func hasExactLine(out, want string) bool {
	for _, l := range strings.Split(out, "\n") {
		if l == want {
			return true
		}
	}
	return false
}

// logPathFrom pulls the path out of the gate's `GATE: full log: <path>` line.
func logPathFrom(out string) string {
	const marker = "GATE: full log: "
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, marker) {
			return strings.TrimSpace(strings.TrimPrefix(l, marker))
		}
	}
	return ""
}

// #299 — a gate that cannot write a log must FAIL, not run the suite blind.
// The shell proof had a dedicated vector for this ('no log possible —
// fail-closed with a FAIL verdict'); nothing replaced it, so weakening the
// recipe's mktemp guard to `|| log=/dev/null` left `make gate` exiting 0 and
// ending in GATE: PASS having captured nothing — a gate that knows nothing
// about the suite reporting a pass.
//
// GATE_CMD is `true` deliberately: the inner command SUCCEEDS, so the only
// thing that can make this non-zero is the log refusal itself.
func TestGateFailsClosedWhenItCannotCreateALog(t *testing.T) {
	needBinary(t, "make")
	cmd := exec.Command("make", "gate", "GATE_CMD=true")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "TMPDIR="+filepath.Join(t.TempDir(), "no", "such", "dir"))
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err == nil {
		t.Fatalf("gate with an unwritable TMPDIR exited 0 without running anything:\n%s%s", out, stderr.String())
	}
	if got := lastLine(out); !strings.Contains(got, "GATE: FAIL") {
		t.Errorf("gate with an unwritable TMPDIR must end in a FAIL verdict, got %q:\n%s", got, out)
	}
	if strings.Contains(out, "GATE: PASS") {
		t.Errorf("gate with an unwritable TMPDIR ended in a PASS verdict:\n%s", out)
	}
}

// #298 — the anti-footgun can be narrowed to nothing by editing one variable.
// Change GATE_DEFAULT_CMD from `$(MAKE) verify` to `$(MAKE) build` and `make
// gate` runs a compile, prints GATE: PASS, and `make verify` (which runs
// gate-proof) stays green. The shell proof got this for free with
// `make -n gate | grep -q verify`; the Go port replaced it with an arm that
// runs the real default command and was skipped unless GATE_PROOF_FULL=1,
// which nothing in the Makefile or CI sets.
//
// Read from the DRY RUN, so the assertion costs nothing and never runs the
// real suite: -n prints the recipe with every variable expanded, and the
// inner command is the one redirected into the log.
func TestGateDefaultRecipeStillRunsVerify(t *testing.T) {
	needBinary(t, "make")
	cmd := exec.Command("make", "-n", "gate")
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("make -n gate could not run: %v", err)
	}
	inner := regexp.MustCompile(`(?m)^\s*(\S.*?)\s*>"\$log"`)
	m := inner.FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("cannot find the gate's inner command in the dry run — this test would "+
			"otherwise report a pass over a recipe it never read:\n%s", out)
	}
	if !regexp.MustCompile(`(^|\s)verify(\s|$)`).MatchString(m[1]) {
		t.Errorf("the DEFAULT gate recipe does not run verify — the gate has been narrowed. "+
			"Inner command: %q", m[1])
	}
}

func TestGateAnnouncesAnOverriddenInnerCommandAndIsSilentOnTheDefault(t *testing.T) {
	needBinary(t, "make")
	_, overridden := runGate(t, "true")
	if !strings.Contains(strings.ToUpper(overridden), "NOTE") {
		t.Errorf("an overridden GATE_CMD must be announced above the verdict — otherwise the "+
			"knob that exists for this proof could silently narrow a real gate run:\n%s", overridden)
	}
	// The other direction: a run that did NOT override must stay silent, or the
	// announcement carries no information at all. This arm was behind an early
	// return unless GATE_PROOF_FULL=1, which neither the Makefile nor either
	// workflow sets (#319) — so the direction that keeps the warning meaningful
	// never ran, and making the echo unconditional left gate-proof green.
	//
	// It runs for real here, and instantly, by SUBSTITUTING the default rather
	// than overriding GATE_CMD: a command-line GATE_DEFAULT_CMD beats the
	// makefile's `:=`, GATE_CMD's `?=` then takes that same value, and the
	// parse-time `ifeq` compares equal — exactly the not-overridden state, with
	// an inner command that returns at once. What the default IS stays pinned
	// separately by TestGateDefaultRecipeStillRunsVerify, off the dry run.
	_, def := runGate(t, "", "GATE_DEFAULT_CMD=true")
	if strings.Contains(strings.ToUpper(def), "NOTE") {
		t.Errorf("a gate run that overrode nothing was announced as overridden — the "+
			"announcement then carries no information:\n%s", def)
	}
	if got := lastLine(def); got != "GATE: PASS" {
		t.Fatalf("the substituted-default run did not reach a verdict, so the silence above "+
			"proves nothing; got %q:\n%s", got, def)
	}
}

func TestGateKeepsTheLogOnFailureAndCleansItOnSuccess(t *testing.T) {
	needBinary(t, "make")
	logPath := filepath.Join(t.TempDir(), "gate.log")

	if _, out := runGate(t, "sh -c 'echo kept-output; exit 1'", "GATE_LOG="+logPath); out == "" {
		t.Fatal("no output from a failing gate")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("a failing gate must KEEP its log so the failure is diagnosable: %v", err)
	}
	if !strings.Contains(string(data), "kept-output") {
		t.Errorf("the kept log must hold the inner output, got %q", string(data))
	}

	// An EXPLICIT GATE_LOG is the operator's path and is kept whatever happens —
	// `created` is set only when the gate had to mktemp one for itself. It is the
	// DEFAULT log that must not survive a green run, since a stale log from a
	// passing run is exactly what a later reader mistakes for evidence.
	explicit := filepath.Join(t.TempDir(), "explicit.log")
	runGate(t, "true", "GATE_LOG="+explicit)
	if _, err := os.Stat(explicit); err != nil {
		t.Errorf("an explicitly named GATE_LOG belongs to the caller and must be kept: %v", err)
	}

	before := gateLogsInTemp(t)
	runGate(t, "true")
	if after := gateLogsInTemp(t); after > before {
		t.Errorf("a passing gate left its DEFAULT log behind (%d -> %d) — a stale log from a "+
			"green run is what a later reader mistakes for evidence", before, after)
	}
}

// gateLogsInTemp counts the gate's own mktemp logs, so the default-log arm can
// tell "cleaned up" from "never created".
func gateLogsInTemp(t *testing.T) int {
	t.Helper()
	dir := os.Getenv("TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	m, err := filepath.Glob(filepath.Join(dir, "formwork-gate.*"))
	if err != nil {
		t.Fatal(err)
	}
	return len(m)
}
