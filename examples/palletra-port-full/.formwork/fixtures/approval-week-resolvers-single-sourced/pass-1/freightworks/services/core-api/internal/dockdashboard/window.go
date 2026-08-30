//go:build ignore

package dockdashboard

import "time"

// Calls the shared single-source resolver — no private re-declaration.
func periodStart(t time.Time) time.Time {
	return signoffstats.WeekStartUTC(t)
}
