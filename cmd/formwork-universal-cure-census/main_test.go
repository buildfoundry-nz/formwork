package main

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/census"
)

// Every fire case is PRESENT-BUT-WRONG by construction: the rule file is
// present, well-formed, and its own gate green — the mis-pairing between an
// existential detector and a universal cure is the violation, not a missing
// file. A census that only noticed deletions could never see these.
func TestDetect(t *testing.T) {
	cases := []struct {
		tree      string
		wantFlags []census.Finding
	}{
		{
			tree: "fire-1",
			wantFlags: []census.Finding{
				{File: ".formwork/rules/demo-universal-cure.yaml", Line: 13, Arm: "demo-sync-handler"},
			},
		},
		{
			// Folded cure: the universal word sits on a YAML continuation
			// line below `cure: >-`, still inside the cure block.
			tree: "fire-2",
			wantFlags: []census.Finding{
				{File: ".formwork/rules/demo-folded-cure.yaml", Line: 13, Arm: "demo-folded-cure"},
			},
		},
		// Honest existential: cure claims presence in one named file.
		{tree: "pass-1"},
		// Universal cure properly paired with mode: every-file.
		{tree: "pass-2"},
		// mode: exists and the universal cure live in DIFFERENT arms (the
		// api-factory-cmd-lifecycle-class shape).
		{tree: "pass-3"},
		// The universal word sits in a COMMENT beside an honest singular
		// cure (the entitlements-audit-10001 shape).
		{tree: "pass-4"},
		{
			// mutation-proof rematerialises keys alphabetically: `id` is
			// not the first key. A `^\s*- id:` scan finds no arm.
			tree: "fire-3",
			wantFlags: []census.Finding{
				{File: ".formwork/rules/demo-id-not-first.yaml", Line: 12, Arm: "demo-id-not-first"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.tree, func(t *testing.T) {
			got, _, err := detect("testdata/" + tc.tree)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.wantFlags) {
				t.Fatalf("detect() = %v, want %v", got, tc.wantFlags)
			}
			for i, w := range tc.wantFlags {
				if got[i] != w {
					t.Errorf("flag %d = %+v, want %+v", i, got[i], w)
				}
			}
		})
	}
}

// A universal word in the arm's PATTERN or scope — anywhere in the arm but
// outside the cure block — must not fire: only the cure can over-promise.
func TestUniversalWordOutsideCure(t *testing.T) {
	lines := []string{
		"rules:",
		"- id: demo",
		"  params:",
		"    pattern: 'every.*handler'", // the word, but it is the DETECTOR
		"    mode: exists",
		"  cure: 'The bootstrap must install the handler.'",
	}
	if cureHasUniversal(lines, 1, len(lines)) {
		t.Error("universal word in params fired; only the cure block may indict")
	}
}
