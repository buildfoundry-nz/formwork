//go:build ignore

package scan

const resolvedQuery = `SELECT COALESCE(adjusted_value, value) AS effective FROM t`
