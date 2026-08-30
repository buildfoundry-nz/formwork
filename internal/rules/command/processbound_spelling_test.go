// processbound_spelling_test.go — #81's residual member.
//
// rules.ProcessBoundOf documents a CONSERVATIVE default: "a CostHeavy checker
// that does not implement ProcessBound defaults to bound — so an unknown heavy
// type cannot fork unbounded analyzers" (internal/rules/rules.go:78).
//
// command implements it, and its implementation inverted that default for the
// one rule type that can spawn an ARBITRARY program. It matched argv0 against
// the literal strings "dart" and "flutter", so every other spelling of the same
// analyzer returned false and went to the FULL-WIDTH pool — the interface is
// fail-closed, its most dangerous implementation was fail-open.
//
// Measured before the fix (formwork @0ca6007e): five of six real spellings
// missed. Measured across the ported corpora: 0 of 135 command rules were
// bound; across the validating target at the pinned SHA, 2 of 86.
//
// The residue is stated in ProcessBound's own comment and is deliberate: a
// wrapper that names no analyzer (`make analyze`) is still undecidable from
// argv. #236 measured that trade and closed it — binding every CostHeavy
// command costs +50% wall-clock, and zero of the 221 command rules across both
// corpora carry an argv0 this function cannot classify, so the flip would buy a
// case that does not occur.
package command_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// Every one of these reaches the same multi-GB analyzer process.
func TestProcessBoundSeesTheAnalyzerThroughItsRealSpellings(t *testing.T) {
	for _, params := range []string{
		"cmd: [dart, analyze]",
		"cmd: [/usr/local/bin/dart, analyze]",        // absolute path
		"cmd: [fvm, dart, analyze]",                  // Flutter Version Manager
		"cmd: [bash, -c, 'dart analyze .']",          // shell wrapper — 110/135 corpus rules use one
		"cmd: [sh, -c, 'cd app && flutter analyze']", // wrapper, not at argv start
		// A DOUBLE-quoted YAML scalar, deliberately: single-quoted YAML does not
		// process escapes and folds a real newline to a space, so the obvious
		// `'set -e\nflutter test'` spelling silently tests a ONE-LINE string and
		// proves nothing about the newline command position this case exists for.
		"cmd: [bash, -c, \"set -e\\nflutter test\\n\"]",
	} {
		if c := build(t, params); !rules.ProcessBoundOf(c) {
			t.Errorf("%s spawns an analyzer but is not process-bound — it runs at full --workers", params)
		}
	}
}

// The narrowing, and it is load-bearing: binding everything would serialise the
// 84 `go` and 110 `bash` rules that are not analyzers at all. These three
// non-invocations are the exact false positives a naive substring scan produced
// when this was measured against the corpora — a python `dart = ...` assignment
// and a `flutter step:` label in a workflow-shaped string.
func TestProcessBoundDoesNotBindANonInvocation(t *testing.T) {
	for _, params := range []string{
		"cmd: [go, run, scripts/dev/check-x.go, .]",
		"cmd: [bash, scripts/dev/reconcile.sh]",
		"cmd: [python3, -c, 'dart = 1']",              // assignment, not a command
		"cmd: [bash, -c, 'echo flutter step: build']", // a label, not an invocation
		"cmd: [true]",
	} {
		if c := build(t, params); rules.ProcessBoundOf(c) {
			t.Errorf("%s is not an analyzer but was bound — it would serialise through the K=1 pool", params)
		}
	}
}

// The ACCEPTED over-approximation, pinned so it is a decision rather than a
// surprise. Inside a shell -c body a data line and a command are textually
// identical: examples/palletra-port-full's flutter-ci-workflow-structure-checked
// carries the table line `dart run melos exec~melos affected-test runner`, which
// only ever gets grepped, and binds anyway.
//
// It is pinned as EXPECTED because the asymmetry decides it — a false positive
// serialises one rule through the K=1 pool; a false negative is the machine hang
// #81 was filed about. If a later change makes this unbind, that is a real
// narrowing and this test should be re-read, not deleted.
func TestShellBodyArmOverApproximates(t *testing.T) {
	c := build(t, "cmd: [bash, -c, \"check() { :; }\\ndart run melos exec~melos affected-test runner\\n\"]")
	if !rules.ProcessBoundOf(c) {
		t.Fatal("expected the documented over-approximation: a data line inside a -c " +
			"body is indistinguishable from an invocation and is bound")
	}
}

// The position narrowing that keeps that over-approximation from spreading. A
// non-shell rule's arguments are DATA, never a script body, so they are not
// scanned at all — measured against examples/palletra-port-full, where a python3
// rule carries `dart = 1` and a workflow-shaped label reads `flutter step:`.
func TestNonShellArgumentsAreNeverScannedAsAScriptBody(t *testing.T) {
	for _, params := range []string{
		"cmd: [python3, -c, \"tbl='dart run melos exec~melos runner'\"]",
		"cmd: [go, run, ./scripts/x.go, \"flutter test( |$)~flutter test step\"]",
	} {
		if c := build(t, params); rules.ProcessBoundOf(c) {
			t.Errorf("%s carries analyzer text as DATA, not a script body — binding it "+
				"would serialise an ordinary rule", params)
		}
	}
}

// The two precision features of analyzerAtCommandPosition, pinned inside a
// shell -c body — the only place the regex is ever consulted. Without these the
// anchor and the terminator can both be deleted and every other test still
// passes, which is a guard that reads as covered and is not.
func TestShellBodyArmRequiresACommandPositionAndASubcommand(t *testing.T) {
	// Not at a command position: prose in an echo. Deleting the leading
	// `(?:^|[\n;&|(`]|\$\()` anchor binds this.
	if c := build(t, "cmd: [bash, -c, \"echo please run dart analyze manually\"]"); rules.ProcessBoundOf(c) {
		t.Error("a mention inside an echo is not an invocation — the command-position anchor is not doing its job")
	}
	// At a command position but followed by punctuation, not a subcommand: a
	// pipe-separated data table in a heredoc. Deleting the trailing
	// `[a-z][a-z0-9-]*(?:[ \t\n]|$)` terminator binds this.
	if c := build(t, "cmd: [bash, -c, \"cat <<EOF\\ndart | melos runner\\nEOF\"]"); rules.ProcessBoundOf(c) {
		t.Error("`dart |` names no subcommand — the terminator is not doing its job")
	}
}
