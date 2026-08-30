// rangesource_internal_test.go — #314.
//
// rangeNonEmpty decides whether a `for … = range src` clause's write is one the
// fold must respect, and it is the ONE place this fix could go wrong in the
// expensive direction: answer true for a source that may iterate zero times and
// the fold deletes a world the program really produces — a true positive, the
// cost that killed this fold's first design (#72).
//
// It is tested here, from inside the package, because two of its arms have no
// reachable fixture. A range over a string yields an index and a rune; a range
// over an int yields an int; the fold tracks only names seeded with a STRING
// literal, so in well-typed Go neither clause can ever write a tracked variable
// and no source through FromGoReassembled can tell those arms apart from a
// bare `return false`. They are answered anyway: rangeNonEmpty is a question
// about the SOURCE, and a pass that resolves no types (spec §2) must not answer
// a source question by reasoning about what the target's type would have to be.
// A predicate that silently means "…except for the sources I assumed could not
// reach me" is the assumption the next range shape breaks.
package sqlextract

import (
	"go/parser"
	"testing"
)

func TestRangeNonEmptyDecidesOnSourceShapeAlone(t *testing.T) {
	arrays := map[string]bool{"arr": true}
	cases := []struct {
		src  string
		want bool
		why  string
	}{
		// A name is non-empty only when the scope proved it an array.
		{"arr", true, "an array name: its length is part of its type"},
		{"xs", false, "an unproven name: a slice, map or channel may be empty"},
		{"(arr)", true, "parentheses do not change what is ranged over"},

		// Literals used directly as the source: nothing sits between the literal
		// and the loop, so a non-empty one cannot be emptied first.
		{`[]string{"a"}`, true, "one element is one iteration"},
		{"[]string{}", false, "an empty slice literal iterates zero times"},
		{"[2]string{}", true, "an array literal's length is its type's, not its elements'"},
		{"[0]string{}", false, "length zero, however it is spelled"},
		{`[...]string{"a"}`, true, "[...] takes its length from the elements"},
		{"map[string]int{}", false, "an empty map literal iterates zero times"},

		// Range over a string. Unreachable for a tracked variable, answered
		// because the question is about the source.
		{`"ab"`, true, "a non-empty string literal yields at least one rune"},
		{"`ab`", true, "a raw string is a string"},
		{`""`, false, "the empty string yields nothing"},
		{"``", false, "the empty raw string yields nothing"},

		// Range over an int, same reason.
		{"3", true, "range over a positive int literal iterates that many times"},
		{"0", false, "range over 0 iterates zero times"},
		{"0x2", true, "a hexadecimal literal is an int literal"},
		{"1_000", true, "digit separators are part of Go's integer syntax"},
		{"-3", false, "a unary expression is not a literal this pass evaluates"},
		{"3.0", false, "a float is not an int, and is not a range source at all"},
		{"99999999999999999999", false,
			"too large for an int: unparseable here, and a compile error there"},

		// Everything else is a value this pass does not resolve.
		{"f()", false, "a call's result is unknown"},
		{"xs[0]", false, "an index expression is unknown"},
		{"pkg.Items", false, "a qualified name is unknown"},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			expr, err := parser.ParseExpr(c.src)
			if err != nil {
				t.Fatalf("parse %q: %v", c.src, err)
			}
			if got := rangeNonEmpty(expr, arrays); got != c.want {
				t.Fatalf("rangeNonEmpty(%s) = %v, want %v — %s", c.src, got, c.want, c.why)
			}
		})
	}
}
