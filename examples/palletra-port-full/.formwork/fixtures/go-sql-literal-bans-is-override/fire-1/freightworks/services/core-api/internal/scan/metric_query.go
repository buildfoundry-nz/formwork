//go:build ignore

package scan

const tallyQuery = `SELECT is_manual_flag FROM palletra.annotation_gauges WHERE page_id = $1` // want: go-sql-literal-bans-is-override
