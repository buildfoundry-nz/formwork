//go:build ignore

package page_layout

func Handle(geo *Geometry) {
	// geo.TextRuns = nil  (commented out — does not count)
	serve(geo)
}
