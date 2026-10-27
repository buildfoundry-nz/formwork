package pattern

import (
	"reflect"
	"regexp"
	"testing"
)

func TestAutoPrefilterNamesWhatEveryMatchMustContain(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		{`json\.Marshal\(`, []string{"json.Marshal("}},
		{`\bfoo.*barbaz\b`, []string{"barbaz"}},
		{`(?:encoding/json|jsoniter)`, []string{"encoding/json", "jsoniter"}},
		{`(Get|Put)Thing\(`, []string{"Thing("}},
		{`retired_(key|field)_x`, []string{"retired_"}},
		{`x(abc)+y`, []string{"abc"}},
	} {
		if got := autoPrefilter(tc.pattern, ""); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("autoPrefilter(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
}

// Each of these has no literal every match must contain, or one the gate
// cannot test honestly. A nil gate runs the rule as before.
func TestAutoPrefilterDeclinesWhatItCannotProve(t *testing.T) {
	for _, tc := range []struct{ pattern, syntax, why string }{
		{`(?i)todo`, "", "case-folded: the gate is a case-sensitive search"},
		{`ab`, "", "too short to skip anything"},
		{`foo|[0-9]+`, "", "one branch guarantees no literal"},
		{`(abc)*`, "", "may match empty"},
		{`\x{FFFD}bad`, "", "U+FFFD also matches invalid UTF-8 the byte search misses"},
		{`(?<=foo)bar`, "regexp2", "regexp2 is outside this analysis"},
		{`a|b|c|d|e|f|g|h|i`, "", "nine branches"},
		{`(`, "", "does not parse"},
	} {
		if got := autoPrefilter(tc.pattern, tc.syntax); got != nil {
			t.Errorf("autoPrefilter(%q, %q) = %q, want nil: %s", tc.pattern, tc.syntax, got, tc.why)
		}
	}
}

// The gate is sound when every string the pattern matches contains one of its
// literals. A gate that fails this skips a file that would have matched.
func FuzzAutoPrefilterIsSound(f *testing.F) {
	for _, seed := range []struct{ pattern, input string }{
		{`json\.Marshal\(`, "x := json.Marshal(v)"},
		{`(?:encoding/json|jsoniter)`, `import "encoding/json"`},
		{`(Get|Put)Thing\(`, "PutThing(1)"},
		{`retired_(key|field)_x`, "a retired_field_x b"},
		{`\bfoo.*barbaz\b`, "foo and barbaz"},
		{`x(abc)+y`, "xabcabcy"},
		{`[a-z]+Error\("`, `return newError("bad")`},
	} {
		f.Add(seed.pattern, seed.input)
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return
		}
		lits := autoPrefilter(pattern, "")
		if lits == nil || !re.MatchString(input) {
			return
		}
		if !containsAny(input, lits) {
			t.Fatalf("pattern %q matches %q, which contains none of the gate's literals %q", pattern, input, lits)
		}
	})
}
