//go:build ignore

package workflow

// composeSections assembles the workflow section rows for the sidebar.
func composeSections(rows []sectionRecord) []sectionRecord {
	var out []sectionRecord
	for _, rs := range rows {
		blankAnnotationList := rs.annotationCount == 0
		// Drop an empty annotation-list section ONLY when its def opts in via
		// the workflowstrategy.CollapseWhenEmpty set — never a section-code compare.
		if blankAnnotationList && workflowstrategy.CollapseWhenEmpty[rs.code] {
			continue
		}
		if blankAnnotationList {
			// Legit empty-state attachment: header + Add button stay visible.
			rs = attachPlaceholderState(rs)
			// The per-section prerequisite-vs-add-first copy compare lives here,
			// and is not on an `blankAnnotationList &&` line.
			if rs.code == partitionWidthsSectionTag {
				rs = withDependencyCopy(rs)
			}
		}
		out = append(out, rs)
	}
	return out
}
