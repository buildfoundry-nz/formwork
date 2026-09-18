// cli_durations_test.go — `check --durations <report>` feeds a previous run's
// per-rule timings back to the scheduler (#26).
//
// The flag carries one promise and one hazard. The promise is that it changes
// nothing an operator can read: same verdict, same findings, same exit code,
// byte for byte, with the flag and without it. The hazard is this repo's
// standing defect class — a supplied flag that is silently ignored, which is
// how an operator comes to believe a run was scheduled, filtered or scoped in a
// way it was not. Every arm below that cannot honour the flag therefore refuses
// (exit 2) rather than proceeding without it.
package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// durationsRepo is a corpus with a finalizer rule (so there is a phase-2 pool
// to order at all) and a per-file rule, one of each so the report carries more
// than one duration. Both pass, so the run exits 0 and the identical-output
// assertion is about a GREEN run — the one an operator sees most.
func durationsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n"+
			"  - id: has-widget\n    type: required-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET, mode: exists}\n"+
			"  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: banana}\n")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const x = \"WIDGET\"\n")
	return root
}

// writeReport runs `check -format json` and saves the report, which is exactly
// how a CI job would produce the file the next run consumes.
func writeReport(t *testing.T, root string) string {
	t.Helper()
	code, out, errOut := runCLI(t, "check", "-C", root, "-format", "json")
	if code != 0 {
		t.Fatalf("producing the report: exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	var rep struct {
		Durations map[string]int64 `json:"durations"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Durations) == 0 {
		t.Fatalf("the report carries no durations, so nothing downstream is being tested:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCheckDurationsLeavesTheReportIdentical is the flag's whole contract on
// the output side. The human format is compared because it carries no timings
// of its own: two runs of the same tree differ in the `durations` object by
// construction (wall time is not reproducible), so comparing JSON would compare
// the clock rather than the verdict.
func TestCheckDurationsLeavesTheReportIdentical(t *testing.T) {
	root := durationsRepo(t)
	report := writeReport(t, root)

	codeA, outA, _ := runCLI(t, "check", "-C", root)
	codeB, outB, _ := runCLI(t, "check", "-C", root, "--durations", report)
	if codeA != codeB {
		t.Fatalf("exit code changed with --durations: %d -> %d", codeA, codeB)
	}
	if outA != outB {
		t.Fatalf("--durations changed the report. It may change how long a run takes and nothing "+
			"else.\nwithout:\n%s\nwith:\n%s", outA, outB)
	}
}

// TestCheckDurationsMissingFileIsExitTwo. The operator asked for a schedule
// built from a file that is not there — a CI job whose artifact download
// failed, a path typo. Running anyway would be a pass produced by a run that
// silently was not the run that was asked for.
func TestCheckDurationsMissingFileIsExitTwo(t *testing.T) {
	root := durationsRepo(t)
	missing := filepath.Join(t.TempDir(), "nope.json")
	code, out, errOut := runCLI(t, "check", "-C", root, "--durations", missing)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for an unreadable durations report\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	// The ARM is asserted, not merely the exit code. Every later arm in
	// priorDurations refuses an unreadable file too — an empty read decodes to
	// no durations — so a test that only checked for exit 2 would pass with this
	// branch deleted, reporting a corrupt report where the file is simply
	// absent. Verified by that mutation: without the phrase below it survives.
	if !strings.Contains(errOut, "--durations") || !strings.Contains(errOut, "cannot read the report") {
		t.Fatalf("the refusal must name the flag and say the file could not be read:\n%s", errOut)
	}
}

// TestCheckDurationsMalformedFileIsExitTwo — a truncated or non-JSON artifact
// is the same refusal. Guessing at a half-written report would schedule from
// whichever rules happened to survive the truncation.
func TestCheckDurationsMalformedFileIsExitTwo(t *testing.T) {
	root := durationsRepo(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	mustWrite(t, bad, `{"findings": [`)
	code, _, errOut := runCLI(t, "check", "-C", root, "--durations", bad)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a malformed durations report\nstderr:\n%s", code, errOut)
	}
	// Again the arm, not just the code: a half-decoded report also carries no
	// durations, so the emptiness arm below would answer this file too — with
	// advice about producing a JSON report, for a file that IS one.
	if !strings.Contains(errOut, "not a formwork JSON report") {
		t.Fatalf("the refusal must say the file did not parse:\n%s", errOut)
	}
}

// TestCheckDurationsWithoutTimingsIsExitTwo is the quiet one, and the reason
// this is a refusal rather than a shrug. A `-format human` report, or a JSON
// report from a `--staged` run, is valid JSON with no `durations` object at
// all — so the flag would be accepted, parsed, and silently do nothing. The
// operator would have no way to tell that from a schedule that was applied.
func TestCheckDurationsWithoutTimingsIsExitTwo(t *testing.T) {
	root := durationsRepo(t)
	empty := filepath.Join(t.TempDir(), "no-durations.json")
	mustWrite(t, empty, `{"findings": [], "summary": {"rules_total": 2}}`)
	code, _, errOut := runCLI(t, "check", "-C", root, "--durations", empty)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a report carrying no durations\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "carries no `durations` object") {
		t.Fatalf("the refusal must say what the report was missing:\n%s", errOut)
	}
}

// TestCheckDurationsRefusedInFileSetModes. Timing is collected on whole-tree
// runs only (0.6.2), and the changeset branch calls engine.Run, which takes no
// hint — so accepting the flag there would accept it and discard it. Same
// answer as --staged with --range, and for the same reason.
func TestCheckDurationsRefusedInFileSetModes(t *testing.T) {
	root := durationsRepo(t)
	report := writeReport(t, root)
	for _, args := range [][]string{
		{"--staged"},
		{"--range", "origin/main..HEAD"},
	} {
		t.Run(args[0], func(t *testing.T) {
			full := append([]string{"check", "-C", root, "--durations", report}, args...)
			code, _, errOut := runCLI(t, full...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2: --durations cannot be honoured in a file-set mode, "+
					"so it must be refused rather than ignored\nstderr:\n%s", code, errOut)
			}
			if !strings.Contains(errOut, "--durations") {
				t.Fatalf("the refusal must name the flag:\n%s", errOut)
			}
		})
	}
}

// TestCheckDurationsNegativeMillisecondsIsExitTwo. The engine writes
// d.Milliseconds() of a measured wall time, which is never negative, so a
// negative entry means the artifact was edited or corrupted between the run
// that wrote it and the run reading it. Sorting on it would not break anything
// — order is not a verdict — which is precisely why it has to be refused here
// rather than absorbed: the file is not what it claims to be, and the next
// thing to trust it might not be a scheduler.
func TestCheckDurationsNegativeMillisecondsIsExitTwo(t *testing.T) {
	root := durationsRepo(t)
	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	mustWrite(t, corrupt, `{"durations": {"has-widget": -12, "no-banana": 3}}`)
	code, _, errOut := runCLI(t, "check", "-C", root, "--durations", corrupt)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a report carrying a negative duration\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "has-widget") {
		t.Fatalf("the refusal must name the offending rule:\n%s", errOut)
	}
}

// TestCheckDurationsReachTheScheduler closes the branch every other test here
// leaves open: that the flag is read, validated, and then dropped on the floor.
// Output is identical with and without the hint by design, so nothing in the
// report can tell a hint that was applied from one that was parsed and
// forgotten — which is the same silent-ignore defect the refusals above exist
// to prevent, one layer in.
//
// -progress is the channel that can see it, and it is an EXISTING one: it
// streams a line per finalizer as it completes (0.6.2), to stderr, never to
// stdout and never to the verdict. At --workers 1 the pool is one worker wide,
// so completion order is dispatch order, and the three rules below are
// finalizers (required-pattern in exists mode evaluates once per run). The hint
// names the LAST-declared rule as the slowest; it must complete first.
func TestCheckDurationsReachTheScheduler(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n"+
			"  - id: r-alpha\n    type: required-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET, mode: exists}\n"+
			"  - id: r-beta\n    type: required-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET, mode: exists}\n"+
			"  - id: r-zulu\n    type: required-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET, mode: exists}\n")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const x = \"WIDGET\"\n")

	hint := filepath.Join(t.TempDir(), "durations.json")
	mustWrite(t, hint, `{"durations": {"r-alpha": 1, "r-beta": 2, "r-zulu": 5000}}`)

	order := func(errOut string) []string {
		var out []string
		for _, line := range strings.Split(errOut, "\n") {
			if _, rest, ok := strings.Cut(line, "progress rule="); ok {
				id, _, _ := strings.Cut(rest, " ")
				out = append(out, id)
			}
		}
		return out
	}

	code, _, errOut := runCLI(t, "check", "-C", root, "--workers", "1", "-progress", "--durations", hint)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, errOut)
	}
	got := order(errOut)
	if len(got) != 3 {
		t.Fatalf("progress named %d finalizer(s), want 3:\n%s", len(got), errOut)
	}
	if got[0] != "r-zulu" {
		t.Fatalf("finalizer order = %v, want the rule the hint measured at 5s first: a --durations "+
			"report that is read and then not handed to the engine schedules nothing, and no report "+
			"output can show the difference (#26)", got)
	}

	// Without the flag the same corpus runs in declaration order — so the
	// assertion above is about the hint, not about something the pool would
	// have done anyway.
	_, _, plain := runCLI(t, "check", "-C", root, "--workers", "1", "-progress")
	if base := order(plain); len(base) != 3 || base[0] != "r-alpha" {
		t.Fatalf("without --durations the order was %v, want declaration order starting at r-alpha; "+
			"the hinted assertion above proves nothing unless this differs", base)
	}
}
