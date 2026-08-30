//go:build ignore

package dockdashboard

import "time"

// A re-hardcoded private copy of a resolver that belongs once in internal/signoffstats.
func weekStartUTC(t time.Time) time.Time { // want: approval-week-resolvers-single-sourced
	return t.AddDate(0, 0, -int(t.Weekday()))
}
