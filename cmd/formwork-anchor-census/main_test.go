package main

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/census"
)

// Every fire case is PRESENT-BUT-WRONG by construction: the rule file is
// present, well-formed, and its own gate green on today's tree — the missing
// anchor is the violation, not a missing file. A census that only noticed
// deletions could never see these.
func TestDetect(t *testing.T) {
	cases := []struct {
		tree      string
		wantFlags []census.Finding
	}{
		{
			tree: "fire-1",
			wantFlags: []census.Finding{
				{File: ".formwork/rules/demo-unanchored.yaml", Line: 8, Arm: "demo-collab-access-audited"},
			},
		},
		{
			// Two count-relation arms in one file; only the unanchored one
			// flags. A sibling arm's funcs: must not satisfy this arm.
			tree: "fire-2",
			wantFlags: []census.Finding{
				{File: ".formwork/rules/demo-cross-arm.yaml", Line: 19, Arm: "demo-unanchored-sibling"},
			},
		},
		// Anchored by func name — the engine asserts it scope-wide.
		{tree: "pass-1"},
		// Anchored by symbol via require_symbol.
		{tree: "pass-2"},
		// Anchor declared in a FLOW mapping on the params: line itself.
		{tree: "pass-3"},
		// Other rule types, whose anchors are required params.
		{tree: "pass-4"},
		{
			// mutation-proof rematerialises keys alphabetically: `id` is
			// not the first key. A `^\s*- id:` scan finds no arm.
			tree: "fire-3",
			wantFlags: []census.Finding{
				{File: ".formwork/rules/demo-id-not-first.yaml", Line: 6, Arm: "demo-id-not-first"},
			},
		},
		{
			// `funcs:` lives only in a comment inside params. A text
			// scan that treats comments as keys would wave this through.
			tree: "fire-4",
			wantFlags: []census.Finding{
				{File: ".formwork/rules/demo-comment-anchor.yaml", Line: 5, Arm: "demo-comment-anchor"},
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

// The anchor keys must sit in the arm's own params block. A `funcs:` named in
// the cure prose or a comment is not this arm's anchor, and accepting it would
// let an arm talk its way out of the ratchet.
func TestAnchorWordOutsideParams(t *testing.T) {
	lines := []string{
		"rules:",
		"- id: demo",
		"  # funcs: was dropped here when the filter was widened",
		"  params:",
		"    left: 'a'",
		"    right: 'b'",
		"    relation: '<='",
		"  cure: 'Set funcs: to the handler name.'",
	}
	if paramsHaveAnchor(lines, 1, len(lines)) {
		t.Error("an anchor key outside the params block satisfied the ratchet")
	}
}

// The params block ends where indentation returns to the key's level: a later
// sibling key must not lend its content to the block.
func TestAnchorAfterParamsBlockDoesNotCount(t *testing.T) {
	lines := []string{
		"- id: demo",
		"  params:",
		"    left: 'a'",
		"    right: 'b'",
		"  origin: scripts/check-funcs:.sh",
	}
	if paramsHaveAnchor(lines, 0, len(lines)) {
		t.Error("content after the params block satisfied the ratchet")
	}
}

// A key whose NAME merely contains an anchor word is not an anchor.
func TestAnchorKeyIsNotASubstringMatch(t *testing.T) {
	lines := []string{
		"- id: demo",
		"  params:",
		"    left: 'a'",
		"    relation: '<='",
	}
	if paramsHaveAnchor(lines, 0, len(lines)) {
		t.Error("a non-anchor key satisfied the ratchet")
	}
}
