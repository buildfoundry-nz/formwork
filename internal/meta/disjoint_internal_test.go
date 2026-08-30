// disjoint_internal_test.go — white-box soundness table for the static half of
// command-trigger-armable (#161). Internal because the predicate's whole value
// is that it is ONE-SIDED: it may answer "cannot prove" for a genuinely
// disjoint pair, but it must never answer "disjoint" for a pair some path
// satisfies. That asymmetry is invisible through lint's output, which only
// shows the verdict the two arms together produced.
package meta

import "testing"

func TestProvablyDisjointIsOneSided(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope []string
		trig  []string
		want  bool
	}{
		// #161's own example: two literal roots that diverge at segment 0.
		{"divergent literal roots", []string{"src/**"}, []string{"db/**"}, true},
		// The trap #161 names. `**/*.sql` has no literal prefix at all, so
		// nothing about it rules out a db/ path — and db/0001.sql satisfies both.
		{"leading doublestar is undecidable", []string{"db/**"}, []string{"**/*.sql"}, false},
		// A shared root: db/migrations/0001.sql satisfies both.
		{"nested under a common root", []string{"db/**"}, []string{"db/migrations/**"}, false},
		// A wildcard-free glob is literal all the way to its leaf, so the
		// comparison reaches the leaf too: no path is both db/a.sql and db/b.sql.
		{"literal leaf names diverge", []string{"db/a.sql"}, []string{"db/b.sql"}, true},
		// The same pair once either side stops being literal: `db/*.sql` has no
		// literal segment past db/, so the leaf divergence is no longer visible
		// and the honest answer is "cannot prove".
		{"wildcard leaf beside a literal one", []string{"db/*.sql"}, []string{"db/b.sql"}, false},
		// Prefix-of-a-segment, not a segment boundary: src/** never matches a
		// srcgen/ path, and segment-wise comparison sees that. A raw
		// strings.HasPrefix over "src" vs "srcgen" would not.
		{"sibling sharing a segment prefix", []string{"src/**"}, []string{"srcgen/**"}, true},
		// A brace alternation begins with a wildcard character, so no literal
		// prefix survives and the pair is undecidable — even though one branch
		// is plainly disjoint from the scope.
		{"brace alternation is undecidable", []string{"src/**"}, []string{"{db,src}/**"}, false},
		// EVERY include must be disjoint from EVERY trigger: one intersecting
		// pair is enough for a path to reach the checker.
		{"one include of several intersects", []string{"src/**", "db/**"}, []string{"db/**"}, false},
		{"all includes diverge", []string{"src/**", "web/**"}, []string{"db/**"}, true},
		// A wildcard-free glob is literal to its leaf, so it can be compared at
		// full depth.
		{"literal path vs divergent root", []string{"src/**"}, []string{"db/schema.sql"}, true},
		// An empty side cannot prove anything; a rule always has an include, so
		// this is a defensive row rather than a reachable one.
		{"no globs", nil, []string{"db/**"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := provablyDisjoint(tc.scope, tc.trig); got != tc.want {
				t.Fatalf("provablyDisjoint(%v, %v) = %v, want %v", tc.scope, tc.trig, got, tc.want)
			}
		})
	}
}
