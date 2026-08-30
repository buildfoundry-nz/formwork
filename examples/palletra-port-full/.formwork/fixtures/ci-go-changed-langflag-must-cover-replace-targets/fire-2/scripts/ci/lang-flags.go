//go:build ignore

package main

import "regexp"

// F5 language path-filter computation.
const goTouchedGlob = "^(freightworks/|shared/generated/)" // LANGFLAG go_touched

// VIOLATION: a SECOND live marker line, left behind when the narrow legacy glob
// was superseded rather than deleted. The extractor concatenates both lines, and
// a two-line pattern is an ALTERNATION to grep -E, so the coverage probes below
// are answered by whichever line happens to cover them. Exactly one line may be
// live, because exactly one is what CI computes the flag from.
const goTouchedGlobLegacy = "^freightworks/" // LANGFLAG go_touched

// goTouched reports whether a changed path falls under the Go job's inputs.
func goTouched(changedPath string) bool {
	return regexp.MustCompile(goTouchedGlob).MatchString(changedPath)
}
