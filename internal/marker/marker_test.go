package marker_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/marker"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		ruleID string
		want   marker.Kind
	}{
		{
			name:   "reasoned marker",
			line:   "bad // formwork:allow no-hit grandfathered until v2",
			ruleID: "no-hit",
			want:   marker.Reasoned,
		},
		{
			name:   "bare marker, no reason",
			line:   "bad // formwork:allow no-hit",
			ruleID: "no-hit",
			want:   marker.Reasonless,
		},
		{
			name:   "block-comment closer only, no real reason",
			line:   "bad /* formwork:allow no-hit */",
			ruleID: "no-hit",
			want:   marker.Reasonless,
		},
		{
			name:   "html-comment closer only, no real reason",
			line:   "bad <!-- formwork:allow no-hit -->",
			ruleID: "no-hit",
			want:   marker.Reasonless,
		},
		{
			name:   "CRLF tail, bare marker",
			line:   "bad // formwork:allow no-hit\r",
			ruleID: "no-hit",
			want:   marker.Reasonless,
		},
		{
			name:   "CRLF tail after a real reason",
			line:   "bad // formwork:allow no-hit grandfathered\r",
			ruleID: "no-hit",
			want:   marker.Reasoned,
		},
		{
			name:   "prefix collision: rule id is a prefix of the marker's id",
			line:   "bad // formwork:allow no-hit a real reason",
			ruleID: "no",
			want:   marker.None,
		},
		{
			name:   "prefix collision: marker's id is a prefix of the rule id",
			line:   "bad // formwork:allow no a real reason",
			ruleID: "no-hit",
			want:   marker.None,
		},
		{
			name:   "reason is only punctuation",
			line:   "bad // formwork:allow no-hit !!!",
			ruleID: "no-hit",
			want:   marker.Reasonless,
		},
		{
			name:   "reason has alphanumerics plus a trailing comment closer",
			line:   "bad /* formwork:allow no-hit grandfathered */",
			ruleID: "no-hit",
			want:   marker.Reasoned,
		},
		{
			name:   "no marker on the line at all",
			line:   "bad line, nothing special",
			ruleID: "no-hit",
			want:   marker.None,
		},
		{
			name:   "marker for a different rule id entirely",
			line:   "bad // formwork:allow other-rule a reason",
			ruleID: "no-hit",
			want:   marker.None,
		},
		{
			name:   "shell comment closer #> only, no real reason",
			line:   "bad # formwork:allow no-hit #>",
			ruleID: "no-hit",
			want:   marker.Reasonless,
		},
		{
			name:   "reason with trailing whitespace only after id",
			line:   "bad // formwork:allow no-hit   ",
			ruleID: "no-hit",
			want:   marker.Reasonless,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := marker.Classify(tc.line, tc.ruleID)
			if got != tc.want {
				t.Fatalf("Classify(%q, %q) = %v, want %v", tc.line, tc.ruleID, got, tc.want)
			}
		})
	}
}
