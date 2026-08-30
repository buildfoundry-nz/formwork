//go:build ignore

package annfold

// mrkByPageQuery value-filters an annotations alias's page_id with NO project_id
// in the same literal — it cannot seek idx_annotation_project_page and scans the
// partition at volume (#7491).
const mrkByPageQuery = `SELECT a.id FROM palletra.annotations a WHERE a.page_id = $1` // want: annotations-table-page-reads-need-project-scope
