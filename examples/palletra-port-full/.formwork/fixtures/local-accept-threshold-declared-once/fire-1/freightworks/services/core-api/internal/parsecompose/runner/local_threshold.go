//go:build ignore

package runner

// local_threshold.go — the local page-type classifier rung.

// defaultLocalApproveThreshold is the confidence at/above which the local rung
// keeps a page instead of escalating to Gemini.
//
// A decoy comment claiming the safe value: // defaultLocalApproveThreshold = 0.99
const defaultLocalApproveThreshold = 0.65

// keep reports whether the local classifier may keep this page.
func keep(conf float64) bool {
	return conf >= defaultLocalApproveThreshold
}
