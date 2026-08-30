//go:build ignore

package skusidebar

func matchEntry(desc string) Result {
	cfg := ratecardmatch.VectorConfig{Threshold: 0.82} // want: ratecard-match-sidebar-no-adhoc-config
	return ratecardmatch.MatchWithVector(desc, cfg)
}
