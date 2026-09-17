package pattern

import "github.com/buildfoundry-nz/formwork/internal/rules/rxmatch"

// The regex backend moved to internal/rules/rxmatch (#17979) so
// pair-consistency compiles through the SAME one instead of calling
// regexp.Compile directly. These aliases keep every call site in this package
// byte-identical, so the extraction is a no-op for the pattern rule types.
type lineMatcher = rxmatch.Matcher

func compileMatcher(what, pattern, syntax string) (lineMatcher, error) {
	return rxmatch.Compile(what, pattern, syntax)
}
