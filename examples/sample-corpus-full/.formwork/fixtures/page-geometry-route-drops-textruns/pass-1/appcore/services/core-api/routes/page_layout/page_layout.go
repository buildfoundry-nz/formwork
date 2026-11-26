//go:build ignore

package page_layout

func Handle(geo *Geometry) {
	geo.TextRuns = nil
	serve(geo)
}
