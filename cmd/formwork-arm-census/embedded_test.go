package main

import (
	"strings"
	"testing"
)

// The real shape, from formula_validate.go: the root-item branch names
// `it.Formula`, and the kit branch two levels down names `kit.Formula`.
var itemAndKitBranches = []string{
	`	if err := validateOneFormula(jobType, sectionKey, it.Description, "item", it.Formula); err != nil {`,
	`		return err`,
	`	}`,
	`	for _, kit := range opt.Kit {`,
	`		if err := validateOneFormula(jobType, sectionKey, kitLoc, "kit", kit.Formula); err != nil {`,
	`			return err`,
	`		}`,
	`	}`,
}

// TestSatisfiedByEmbeddedOnlyFlagsTheItemBranchArm is the shipped defect.
// `it\.Formula` is a substring of `kit.Formula`, so deleting the whole
// root-item branch — the #8812 regression the arm exists to catch — left the
// arm green off the kit branch's text.
func TestSatisfiedByEmbeddedOnlyFlagsTheItemBranchArm(t *testing.T) {
	orig, stripped, yes, err := satisfiedByEmbeddedOnly(mustCompile(t, `it\.Formula`), itemAndKitBranches)
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Fatal("`it\\.Formula` survives inside `kit.Formula` but the arm was spared")
	}
	if !strings.Contains(orig, "kit.Formula") {
		t.Errorf("the surviving line %q is not the kit branch", orig)
	}
	if !strings.Contains(stripped, "kit.Formula") {
		t.Errorf("the stripped line %q no longer carries the text holding the arm up", stripped)
	}
}

// TestSatisfiedByEmbeddedOnlySparesTheBoundArm is the cure, and the half that
// decides whether the check can be kept green. Binding the pattern to its own
// call site leaves it with an aligned witness and no survivor.
func TestSatisfiedByEmbeddedOnlySparesTheBoundArm(t *testing.T) {
	_, _, yes, err := satisfiedByEmbeddedOnly(mustCompile(t, `"item", it\.Formula`), itemAndKitBranches)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Fatal("the bound arm has no embedded survivor but was flagged")
	}
}

// TestSatisfiedByEmbeddedOnlySparesADeliberateSubstring — a pattern with NO
// token-aligned witness is a substring spelling on purpose (`Repo` meaning
// `UserRepo`, `OrderRepo`). Deleting those identifiers DOES fail it, so it is
// not in this class. Flagging it would put dozens of honest arms on the
// report and the rule would be disabled inside a week.
func TestSatisfiedByEmbeddedOnlySparesADeliberateSubstring(t *testing.T) {
	lines := []string{
		"	users := NewUserRepo(db)",
		"	orders := NewOrderRepo(db)",
	}
	_, _, yes, err := satisfiedByEmbeddedOnly(mustCompile(t, `Repo`), lines)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Fatal("a pattern whose every witness is embedded is a deliberate substring spelling, not this defect")
	}
}

// TestSatisfiedByEmbeddedOnlySparesAnAlignedOnlyArm — the ordinary case. Every
// witness stands as its own token, so deleting them all fails the arm.
func TestSatisfiedByEmbeddedOnlySparesAnAlignedOnlyArm(t *testing.T) {
	lines := []string{
		"	if err := ValidateSpecFormulas(spec); err != nil {",
		"		return err",
	}
	_, _, yes, err := satisfiedByEmbeddedOnly(mustCompile(t, `ValidateSpecFormulas`), lines)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Fatal("an arm whose every witness is its own token was flagged")
	}
}

// TestTokenAlignedReadsBothEnds — a match is part of a longer token if EITHER
// end abuts an identifier character. `it.Formula` inside `kit.Formula` abuts
// on the left only, which is the shipped spelling; a check reading only the
// right end waves it straight through.
func TestTokenAlignedReadsBothEnds(t *testing.T) {
	const kit = "kit.Formula"
	if tokenAligned(kit, 1, len(kit)) {
		t.Error("`it.Formula` inside `kit.Formula` abuts `k` on the left and is not token-aligned")
	}
	const longer = "it.FormulaText"
	if tokenAligned(longer, 0, len("it.Formula")) {
		t.Error("`it.Formula` inside `it.FormulaText` abuts `T` on the right and is not token-aligned")
	}
	const own = `"item", it.Formula)`
	if !tokenAligned(own, strings.Index(own, "it.Formula"), len(own)-1) {
		t.Error("a match bounded by a space and a paren is its own token")
	}
}

// TestTokenAlignedReadsBothSidesOfEachEnd — a token only continues where two
// identifier characters MEET. Testing the neighbour alone, without asking what
// the match's own edge character is, calls `ref\.onDispose\(` embedded in
// `ref.onDispose(authListenable.dispose)` because a letter follows the match —
// but the match ends at `(`, which continues nothing. Measured on this corpus
// that one-sided test flagged 5 honest arms out of 7.
func TestTokenAlignedReadsBothSidesOfEachEnd(t *testing.T) {
	const call = "    ref.onDispose(authListenable.dispose);"
	s := strings.Index(call, "ref.onDispose(")
	if !tokenAligned(call, s, s+len("ref.onDispose(")) {
		t.Error("a match ending at `(` is token-aligned however its neighbour reads")
	}
	const open = "	if scoreQuantity(lines) > 0 {"
	o := strings.Index(open, "scoreQuantity(")
	if !tokenAligned(open, o, o+len("scoreQuantity(")) {
		t.Error("a call-site pattern ending at its own paren must never be called embedded")
	}
	// The other direction still holds: two identifier characters meeting IS a
	// continued token, and that is the shipped defect.
	const req = "	req := &apiv1.EnqueueUngroupedMaterialGroupingRequest{}"
	r := strings.Index(req, "EnqueueUngroupedMaterialGrouping")
	if tokenAligned(req, r, r+len("EnqueueUngroupedMaterialGrouping")) {
		t.Error("a name that continues into `Request` is not its own token")
	}
}

// TestSatisfiedByEmbeddedOnlySparesADifferentAlternative — the survivor must be
// the SAME TEXT as an aligned witness. An alternation with one alternative
// standing aligned and a DIFFERENT one surviving is two obligations, not one
// literal doing double duty, and there is nothing an author could bind. On this
// corpus that gate is the difference between 2 flagged and 1.
func TestSatisfiedByEmbeddedOnlySparesADifferentAlternative(t *testing.T) {
	lines := []string{
		"import 'package:tqs_core/testing/stub_dio.dart';",
		"import 'package:takeoffqs_schema/takeoffqs/api/v1/account_service.pb.dart';",
	}
	_, _, yes, err := satisfiedByEmbeddedOnly(mustCompile(t, `(package:takeoffqs_schema|package:tqs_)`), lines)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Fatal("a surviving alternative that no aligned witness shares is not this defect")
	}
}

// TestSatisfiedByEmbeddedOnlyFlagsATypeLiteralStandIn is the corpus's own live
// example, and the reason the same-text gate does not gut the check.
// `settle-grouping-has-live-caller` requires a live CALLER of
// EnqueueUngroupedMaterialGrouping; delete the call and the arm stays green off
// the generated `...GroupingRequest` type literal, which is the same text
// continuing into a longer identifier.
func TestSatisfiedByEmbeddedOnlyFlagsATypeLiteralStandIn(t *testing.T) {
	lines := []string{
		"	n, err := extractionreaper.EnqueueUngroupedMaterialGrouping(",
		"	req := &apiv1.EnqueueUngroupedMaterialGroupingRequest{}",
	}
	orig, _, yes, err := satisfiedByEmbeddedOnly(mustCompile(t, `EnqueueUngroupedMaterialGrouping`), lines)
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Fatal("a type literal standing in for the live caller was not flagged")
	}
	if !strings.Contains(orig, "Request{}") {
		t.Errorf("the surviving line %q is not the type literal", orig)
	}
}
