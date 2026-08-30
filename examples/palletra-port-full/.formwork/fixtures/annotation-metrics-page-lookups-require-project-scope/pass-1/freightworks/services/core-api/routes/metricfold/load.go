//go:build ignore

package metricfold

// tallyByPageQuery carries the project_id predicate alongside page_id in the
// same literal, so the read seeks idx_gauges_project_page (project_id leads).
const tallyByPageQuery = `SELECT am.value FROM palletra.annotation_gauges am WHERE am.project_id = $1 AND am.page_id = $2`
