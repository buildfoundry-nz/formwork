//go:build ignore

package annotationmetrics

// Reads via scanmetric.MetricFromAnnotationTable but with a page_id-leading WHERE
// that bypasses idx_gauges_project_page.
const pageRowQuery = "SELECT " + scanmetric.MarkerTallyColumns + " FROM palletra.annotation_gauges am WHERE am.page_id = $1" // want: annotation-metrics-page-filter-must-use-shared-where-constant
