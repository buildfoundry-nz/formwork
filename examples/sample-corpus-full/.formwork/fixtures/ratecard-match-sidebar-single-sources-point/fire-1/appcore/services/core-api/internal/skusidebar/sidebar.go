//go:build ignore

package skusidebar

// FIRE: the operating point is not single-sourced from TunedEmbedConfig().
func matchEntry(desc string) Result {
	return ratecardmatch.MatchWithVector(desc, defaultCfg)
}
