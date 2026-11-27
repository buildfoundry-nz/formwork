//go:build ignore

package parsewrite

// locateRun finds the active run for a stage-event insert.
func locateRun() string {
	return `SELECT id FROM extraction_attempts WHERE org_id = $1 AND status IN ('pending','running') ORDER BY started_at DESC LIMIT 1` // want: no-status-gated-stage-event-lookup
}
