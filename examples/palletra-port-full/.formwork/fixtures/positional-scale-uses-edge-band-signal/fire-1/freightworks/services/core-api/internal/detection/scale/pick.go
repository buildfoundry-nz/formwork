//go:build ignore

package scale

func extractScaleFromPageContent(pages []*domainv1.PdfPageText) scale.Result {
	return chooseByRanking(gatherScaleMatches(pages))
}

func chooseByRanking(ms []match) match { return ms[0] }

func gatherScaleMatches(pages []*domainv1.PdfPageText) []match { return nil }

func weight(item pdftrace.Item, page pdftrace.Page) bool {
	return true /* geometry signal dropped */
}
