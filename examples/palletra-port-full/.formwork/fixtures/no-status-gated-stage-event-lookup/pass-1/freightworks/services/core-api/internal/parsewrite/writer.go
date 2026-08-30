//go:build ignore

package parsewrite

func locateRun() string {
	return `SELECT id FROM extraction_attempts WHERE org_id = $1 ORDER BY started_at DESC LIMIT 1`
}
