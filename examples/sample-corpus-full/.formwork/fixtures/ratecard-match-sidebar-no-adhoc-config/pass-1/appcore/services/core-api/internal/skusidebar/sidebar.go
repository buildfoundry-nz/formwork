//go:build ignore

package skusidebar

func matchEntry(desc string) Result {
	cfg := ratecardmatch.TunedEmbedConfig()
	return ratecardmatch.MatchWithVector(desc, cfg)
}
