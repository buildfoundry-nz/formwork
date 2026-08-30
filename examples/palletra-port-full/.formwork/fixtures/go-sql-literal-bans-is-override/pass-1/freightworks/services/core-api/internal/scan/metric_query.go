//go:build ignore

package scan

const tallyQuery = `SELECT COALESCE(adjusted_value, value) AS effective FROM palletra.annotation_gauges WHERE page_id = $1`
