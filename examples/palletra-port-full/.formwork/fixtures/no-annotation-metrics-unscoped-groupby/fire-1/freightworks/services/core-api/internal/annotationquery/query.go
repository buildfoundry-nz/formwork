//go:build ignore

package annotationquery

// amendableQuery LEFT JOINs an unscoped aggregate: FROM the metrics table with a
// GROUP BY annotation_id and NO WHERE, so RLS HashAggregates every metric row in
// the org on every annotation read (#3451).
const amendableQuery = `SELECT am.annotation_id, bool_and(am.approved) AS all_cleared FROM palletra.annotation_gauges am GROUP BY am.annotation_id` // want: no-annotation-metrics-unscoped-groupby
