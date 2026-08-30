//go:build ignore

package metricfold

// tallyByPageQuery value-filters an annotation_gauges alias's page_id with NO
// project_id in the same literal — it cannot seek idx_gauges_project_page and
// seq-scans the table at volume (#5692).
const tallyByPageQuery = `SELECT am.value FROM palletra.annotation_gauges am WHERE am.page_id = $1` // want: annotation-metrics-page-lookups-require-project-scope
