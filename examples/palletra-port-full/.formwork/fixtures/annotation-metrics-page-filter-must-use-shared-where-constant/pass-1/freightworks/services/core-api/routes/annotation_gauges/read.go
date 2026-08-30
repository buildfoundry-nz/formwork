//go:build ignore

package annotationmetrics

// Filters via a project_id-leading WHERE (scanmetric.MarkerTallyByPageWhere)
// so the read seeks idx_gauges_project_page.
const pageRowQuery = "SELECT " + scanmetric.MarkerTallyColumns + " FROM palletra.annotation_gauges am WHERE am.project_id = $1 AND am.page_id = $2"
