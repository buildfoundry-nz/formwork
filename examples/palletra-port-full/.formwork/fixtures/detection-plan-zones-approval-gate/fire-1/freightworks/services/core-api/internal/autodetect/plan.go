//go:build ignore

package autodetect

func composeDispatchPlan(pages []Page, talliesByPage map[string]Metrics) RoutePlan {
	var plan RoutePlan
	zonePages := shortlistRoomPages(pages)
	pages = pagesPassingZonesApproval(zonePages, talliesByPage)
	pairs := candidateGantryPairs(pages)
	pairs = gantryPairsPassingApproval(pairs, talliesByPage)
	return assemble(plan, pages, pairs)
}

func pagesPassingZonesApproval(pages []Page, talliesByPage map[string]Metrics) []Page {
	return pages
}

func gantryPairsPassingApproval(pairs []Pair, talliesByPage map[string]Metrics) []Pair {
	floorGated, _ := detectiongates.CheckGantriesApprovalGate(pairs, nil, talliesByPage)
	return excludingPairs(pairs, floorGated)
}
