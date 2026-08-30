//go:build ignore

package pages

// pg.status is retired; read pg.intake_status instead.
func load(pg sheetRow) string {
	return pg.processingStatus
}

type sheetRow struct{ processingStatus string }
