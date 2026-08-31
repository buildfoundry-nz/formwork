package pattern

import (
	"fmt"
	"regexp"
	"strings"
)

// Polarity: suppressing a match whose text DENIES the topic it matched (#4).
//
// A forbidden-pattern matches topic, not polarity, and RE2 has no lookbehind,
// so the exclusion cannot be written inside params.pattern. This is the second
// stage a rule opts into with params.denied_by.
//
// IT MAY ONLY EVER SUPPRESS. Anything undecidable reaches the verdict the rule
// would have reached anyway — a marker the set does not carry, a denial spread
// beyond the window, a match whose position cannot be located. That is what
// makes it safe to ship without a long calibration: it cannot make any existing
// rule weaker than it is today.
//
// THE CLAUSE BOUNDARY IS CARRIED BY THE ALPHABET, NOT BY A SEPARATE RULE.
// Between the marker and the match only spaces, tabs and at most one short word
// may stand, so punctuation and newlines end the clause by construction:
// "we never ship that. rewriting …" does not suppress, and a marker on the
// previous line does not carry. There is no sentence splitter to disagree with.
type denial struct {
	re   *regexp.Regexp
	srcs []string
}

// denialWordMax bounds the one word allowed between marker and match. The
// errors are not symmetric: a denial the window misses costs a re-word, while a
// window loose enough to swallow a real finding costs a violation shipped.
const denialWordMax = 12

func newDenial(markers []string) (*denial, error) {
	if len(markers) == 0 {
		return nil, nil
	}
	alts := make([]string, 0, len(markers))
	for _, m := range markers {
		m = strings.TrimSpace(m)
		if m == "" {
			return nil, fmt.Errorf("denied_by: empty marker")
		}
		if strings.ContainsAny(m, ".*+?()[]{}|^$\\") {
			return nil, fmt.Errorf("denied_by: %q must be literal text, not a pattern — "+
				"the markers are a small closed set, and a regex here would reintroduce "+
				"the ambiguity this stage exists to remove", m)
		}
		alts = append(alts, regexp.QuoteMeta(m))
	}
	// Anchored at the END of the prefix, so the marker must sit immediately
	// before the match. \b at the front is what stops "cannot" from ending in
	// the marker "not": the prefix is handed over UNTRIMMED precisely so this
	// boundary is decided on real text rather than on a slice.
	src := `(?i)\b(?:` + strings.Join(alts, "|") + `)[ \t]+(?:\w{1,` +
		fmt.Sprint(denialWordMax) + `}[ \t]+)?$`
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("denied_by: %w", err)
	}
	return &denial{re: re, srcs: markers}, nil
}

// deniedAt reports whether the match starting at idx in s is denied by the text
// immediately before it. idx < 0 means the position is unknown, which is
// undecidable and therefore not a denial.
func (d *denial) deniedAt(s string, idx int) bool {
	if d == nil || idx < 0 || idx > len(s) {
		return false
	}
	return d.re.MatchString(s[:idx])
}
