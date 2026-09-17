package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// costMaxRepo: one fast rule, one command rule declared `range` whose tool
// fires, one undeclared (heavy) command rule whose tool fires.
func costMaxRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "fast.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "range.yaml"),
		"rules:\n  - id: commit-order\n    type: command\n    scope: {include: ['**']}\n"+
			"    params:\n      cmd: [\"false\"]\n      cost: range\n      expect: {exit: 0}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "heavy.yaml"),
		"rules:\n  - id: whole-tree-census\n    type: command\n    scope: {include: ['**']}\n"+
			"    params:\n      cmd: [\"false\"]\n      expect: {exit: 0}\n")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "package p\n")
	return root
}

// --cost-max range runs the fast rule and the range escape, and DROPS the
// heavy one with a disclosed reason — the one thing --skip-escapes cannot
// express (#22). The range rule's tool fires, so the run is exit 1: the
// dropped rule cannot mask a finding from a rule that did run.
func TestCheckCostMaxRunsTheCheapEscapesAndDisclosesTheRest(t *testing.T) {
	root := costMaxRepo(t)
	code, out, errOut := runCLI(t, "check", "-C", root, "--cost-max", "range")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — the range rule ran and fired\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "[commit-order] FAIL") {
		t.Fatalf("the range-class escape must RUN under --cost-max range:\n%s", out)
	}
	if !strings.Contains(out, "[no-widget] OK") {
		t.Fatalf("the fast rule must still run:\n%s", out)
	}
	if strings.Contains(out, "[whole-tree-census] FAIL") {
		t.Fatalf("the heavy rule must be dropped under --cost-max range:\n%s", out)
	}
	if !strings.Contains(out, "whole-tree-census") || !strings.Contains(out, "--cost-max") {
		t.Fatalf("the drop must be disclosed naming the rule and the flag:\n%s", out)
	}

	code, out, _ = runCLI(t, "check", "-C", root, "--cost-max", "range", "-format", "json")
	rep := decodeNotRun(t, out)
	if len(rep) != 1 || rep[0].Rule != "whole-tree-census" || rep[0].Channel != "cost-max" {
		t.Fatalf("the drop must carry its own channel: %+v\n%s", rep, out)
	}
	_ = code
}

// --cost-max heavy is the whole set; --skip-escapes still drops every escape
// regardless of declared class, so the two flags cannot be combined.
func TestCheckCostMaxHeavyRunsEverything_AndTheFlagsAreExclusive(t *testing.T) {
	root := costMaxRepo(t)
	code, out, _ := runCLI(t, "check", "-C", root, "--cost-max", "heavy")
	if code != 1 || !strings.Contains(out, "[whole-tree-census] FAIL") || !strings.Contains(out, "[commit-order] FAIL") {
		t.Fatalf("--cost-max heavy must run every rule; exit=%d\n%s", code, out)
	}
	code, out, _ = runCLI(t, "check", "-C", root, "--skip-escapes")
	if code != 0 || strings.Contains(out, "[commit-order] FAIL") {
		t.Fatalf("--skip-escapes must still drop a declared range escape; exit=%d\n%s", code, out)
	}
	code, _, errOut := runCLI(t, "check", "-C", root, "--cost-max", "range", "--skip-escapes")
	if code != 2 || !strings.Contains(errOut, "cost-max") {
		t.Fatalf("--cost-max with --skip-escapes must be refused (exit 2) naming the flag; exit=%d\n%s", code, errOut)
	}
	code, _, errOut = runCLI(t, "check", "-C", root, "--cost-max", "cheap")
	if code != 2 || !strings.Contains(errOut, "cheap") {
		t.Fatalf("an unknown class must be exit 2 naming it; exit=%d\n%s", code, errOut)
	}
}
