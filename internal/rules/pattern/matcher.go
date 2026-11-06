package pattern

import (
	"github.com/buildfoundry-nz/formwork/internal/cputime"
	"github.com/buildfoundry-nz/formwork/internal/rules/rxmatch"
)

// The regex backend lives in internal/rules/rxmatch (#17979) so
// pair-consistency compiles through the SAME one instead of calling
// regexp.Compile directly. This alias keeps the pattern rule types on that one
// backend and its mandatory CPU meter.
type lineMatcher = rxmatch.Matcher

func compileMatcher(what, pattern, syntax string, meter *cputime.Meter) (lineMatcher, error) {
	return rxmatch.Compile(what, pattern, syntax, meter)
}
