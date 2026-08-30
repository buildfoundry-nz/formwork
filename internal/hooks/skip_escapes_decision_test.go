// skip_escapes_decision_test.go — #338 (#81 In: bullet 3).
//
// #81's third scope item asked whether `hooks install` and/or --skip-escapes's
// default should change so the common multi-checkout case is safe without
// per-consumer configuration. Two of #81's three items landed; this one was
// neither done nor declined, and nothing anywhere recorded that it had been
// considered. #338 is that silence, not the behaviour.
//
// The decision is NO, reasoned at internal/hooks/hooks.go's checkCommand. This
// file holds the decision to its two observable consequences, so a later change
// has to move the argument rather than quietly land the opposite.
//
// It deliberately does not assert the comment text — prose drifts and a test
// over prose teaches people to edit the test. It asserts the two things a
// consumer can actually observe.
package hooks

import (
	"strings"
	"testing"
)

// TestInstalledHookDoesNotSkipEscapes pins the decision's first consequence:
// the command written into a git hook judges the same rule set a bare
// `formwork check` judges.
//
// Emitting --skip-escapes here would make every installed pre-commit hook stop
// running heavy rules at install time, in no output, for every consumer — a
// gate reporting a pass having declined to look. If a future change decides
// the multi-checkout cost is worth that, it changes this test and states why.
func TestInstalledHookDoesNotSkipEscapes(t *testing.T) {
	for _, lane := range []string{"pre-commit", "pre-push", "commit-msg"} {
		got := checkCommand(lane)
		if strings.Contains(got, "--skip-escapes") {
			t.Errorf("hooks install emits %q for lane %q.\n"+
				"#338/#81 decided against this: skipping escapes in the installed hook narrows what "+
				"the gate judges for every consumer, silently and at install time. The bound for the "+
				"parallel-checkout case is per-lane `cost:` filters, which narrow nothing. Move the "+
				"argument at hooks.go's checkCommand before changing this.", got, lane)
		}
	}
}

// TestCheckCommandIsTheCommandFormworkWouldRun pins the second consequence, and
// the reason the first one matters: the hook's command is the advice formwork
// gives an operator whose existing hook it refuses to replace. If the two
// diverge, the operator debugs the advice instead of the finding.
func TestCheckCommandIsTheCommandFormworkWouldRun(t *testing.T) {
	got := checkCommand("pre-commit")
	if !strings.HasPrefix(got, "formwork check --lane pre-commit") {
		t.Fatalf("checkCommand(\"pre-commit\") = %q, want it to start with the plain "+
			"`formwork check --lane pre-commit` an operator can run by hand", got)
	}
	for _, flag := range []string{"--skip-escapes", "--workers", "--format"} {
		if strings.Contains(got, flag) {
			t.Errorf("checkCommand carries %s (%q) — the emitted command must stay the one a human "+
				"can paste, not a tuned invocation whose behaviour differs from a bare run", flag, got)
		}
	}
}
