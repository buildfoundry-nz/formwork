//go:build ignore

package scan

const resolvedQuery = `SELECT CASE WHEN am.adjusted_value IS NOT NULL THEN adjusted_value ELSE value END FROM t` // want: go-sql-bans-then-override-value
