//go:build ignore

package skusidebar

// FIRE: the sidebar read never runs the fuzzy tier — no MatchWithVector call.
func matchEntry(desc string) Result {
	return ratecardmatch.Match(desc)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// return ratecardmatch.MatchWithVector(desc, cfg)
