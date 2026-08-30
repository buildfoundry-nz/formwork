//go:build ignore

package workflow

// composeSections assembles the workflow section rows for the sidebar.
func composeSections(rows []sectionRecord) []sectionRecord {
	var out []sectionRecord
	for _, rs := range rows {
		blankAnnotationList := rs.annotationCount == 0
		// BUG: the empty-section drop is gated on a hand-rolled section-code
		// allowlist (a named constant), not the CollapseWhenEmpty set — so every
		// other empty draw section is silently dropped.
		if blankAnnotationList && rs.code != partitionWidthsSectionTag { // want: empty-workflow-sections-stay-rendered
			continue
		}
		out = append(out, rs)
	}
	return out
}
