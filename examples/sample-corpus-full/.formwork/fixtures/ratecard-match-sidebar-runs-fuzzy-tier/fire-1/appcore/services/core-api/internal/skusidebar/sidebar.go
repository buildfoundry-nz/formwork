//go:build ignore

package skusidebar

// FIRE: the sidebar read never runs the fuzzy tier — no MatchWithVector call.
func matchEntry(desc string) Result {
	return ratecardmatch.Match(desc)
}
