//go:build ignore

package main

import "regexp"

// F5 language path-filter computation.
const goTouchedGlob = "^freightworks/" // LANGFLAG go_touched

// goTouched reports whether a changed path falls under the Go job's inputs.
func goTouched(changedPath string) bool {
	return regexp.MustCompile(goTouchedGlob).MatchString(changedPath)
}
