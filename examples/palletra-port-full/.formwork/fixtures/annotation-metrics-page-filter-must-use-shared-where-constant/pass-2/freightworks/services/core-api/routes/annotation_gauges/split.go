//go:build ignore

package annotationmetrics

// Reads via scanmetric.MetricFromAnnotationTable, but the WHERE is split
// across lines in a raw string. Line-anchored matching (the .sh's per-line
// grep, restored when the rule left multiline mode) deliberately does not see
// this shape — this fixture pins that narrowing as a decision, not an
// accident. See the rule comment.
const pageRowQuery = `SELECT id FROM palletra.annotation_gauges am WHERE
	am.page_id = $1`
