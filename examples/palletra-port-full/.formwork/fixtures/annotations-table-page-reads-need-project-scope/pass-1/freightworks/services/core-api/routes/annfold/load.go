//go:build ignore

package annfold

// mrkByPageQuery carries the project_id predicate alongside page_id in the same
// literal, so the read seeks idx_annotation_project_page (project_id leads page_id).
const mrkByPageQuery = `SELECT a.id FROM palletra.annotations a WHERE a.project_id = $1 AND a.page_id = $2`
