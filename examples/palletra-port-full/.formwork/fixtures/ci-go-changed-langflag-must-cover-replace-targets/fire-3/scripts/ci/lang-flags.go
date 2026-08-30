//go:build ignore

package main

import "regexp"

// VIOLATION: the marker was commented out during a refactor and never restored,
// so nothing ties the glob CI computes to the glob this gate reads. With no live
// marker the extractor has nothing to check and must say so rather than pass.
// const goTouchedGlob = "^(freightworks/|shared/generated/)" // LANGFLAG go_touched

// F5 language path-filter computation.
const goTouchedGlob = "^freightworks/"

// goTouched reports whether a changed path falls under the Go job's inputs.
func goTouched(changedPath string) bool {
	return regexp.MustCompile(goTouchedGlob).MatchString(changedPath)
}
