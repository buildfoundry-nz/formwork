//go:build ignore

package pages

// pages.status was the old column — this comment must NOT fire (decomment-go).
func load(pg sheetRow) string {
	return pg.status // want: pages-status-column-read-banned
}

type sheetRow struct{ status string }
