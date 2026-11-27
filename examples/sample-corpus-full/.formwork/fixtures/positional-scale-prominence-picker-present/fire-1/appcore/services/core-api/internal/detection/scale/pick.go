//go:build ignore

package scale

func extractScaleFromPageContent(pages []*domainv1.PdfPageText) scale.Result {
	return chooseByRanking(gatherScaleMatches(pages))
}

func legacy_pickByProminence(ms []match) match { return ms[0] }

func gatherScaleMatches(pages []*domainv1.PdfPageText) []match { return nil }

func weight(item pdftrace.Item, page pdftrace.Page) bool {
	return pdftrace.InBorderZone(item.BBox, page)
}
