//go:build ignore

package autodetect

func composeDispatchPlan(pages []Page, talliesByPage map[string]Metrics) RoutePlan {
	var plan RoutePlan
	zonePages := shortlistRoomPages(pages)
	pages = zonePages
	pairs := candidateGantryPairs(pages)
	pairs = gantryPairsPassingApproval(pairs, talliesByPage)
	return assemble(plan, pages, pairs)
}

func pagesPassingZonesApproval(pages []Page, talliesByPage map[string]Metrics) []Page {
	blocked, _ := detectiongates.CheckZonesApprovalGate(pages, talliesByPage)
	return without(pages, blocked)
}

func gantryPairsPassingApproval(pairs []Pair, talliesByPage map[string]Metrics) []Pair {
	floorGated, _ := detectiongates.CheckGantriesApprovalGate(pairs, nil, talliesByPage)
	return excludingPairs(pairs, floorGated)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// pages = pagesPassingZonesApproval(zonePages, talliesByPage)
