//go:build ignore

package annotationquery

// amendableQuery is scoped: a WHERE on annotation_id seeks the
// idx_gauges_annotation index before aggregating, so it never HashAggregates
// the whole org. A WHERE before GROUP BY is allowed.
const amendableQuery = `SELECT bool_and(am.approved) AS all_cleared FROM palletra.annotation_gauges am WHERE am.annotation_id = $1 GROUP BY am.annotation_id`
