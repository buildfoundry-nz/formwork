package engine

import "testing"

func TestHeavyPoolWidths(t *testing.T) {
	cases := []struct {
		workers, nBound, nWide int
		wantBound, wantWide    int
	}{
		{4, 0, 230, 0, 4},  // no Dart: cheap commands keep full width
		{4, 5, 0, 2, 0},    // only Dart: cap 2
		{4, 5, 230, 2, 2},  // CI 4 vCPU: 2 Dart + 2 go run, not 2+4
		{2, 5, 230, 1, 1},  // share two slots
		{10, 5, 230, 2, 8}, // bound still caps at 2
	}
	for _, tc := range cases {
		gotB, gotW := heavyPoolWidths(tc.workers, tc.nBound, tc.nWide)
		if gotB != tc.wantBound || gotW != tc.wantWide {
			t.Fatalf("heavyPoolWidths(%d,%d,%d)=(%d,%d) want (%d,%d)",
				tc.workers, tc.nBound, tc.nWide, gotB, gotW, tc.wantBound, tc.wantWide)
		}
	}
}
