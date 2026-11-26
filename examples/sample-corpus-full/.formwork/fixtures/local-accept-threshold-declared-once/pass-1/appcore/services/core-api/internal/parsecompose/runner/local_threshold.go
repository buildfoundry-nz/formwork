//go:build ignore

package runner

// local_threshold.go — the local page-type classifier rung.

// defaultLocalApproveThreshold is the confidence at/above which the local rung
// keeps a page instead of escalating to Gemini. It stays at the basically-
// positive floor so a coin-flip page always defers to the Gemini authority.
const defaultLocalApproveThreshold = 0.95

// keep reports whether the local classifier may keep this page.
func keep(conf float64) bool {
	return conf >= defaultLocalApproveThreshold
}
