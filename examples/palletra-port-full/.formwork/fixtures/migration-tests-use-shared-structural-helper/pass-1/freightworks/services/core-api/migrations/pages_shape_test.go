//go:build ignore

package migrations

import (
	"strings"
	"testing"
)

func TestPagesMigrationLayout(t *testing.T) {
	// Uses the shared helper — no os.ReadFile preamble here.
	sql := lowerSQL(t, "0042_pages.sql")
	if !strings.Contains(sql, "create table") {
		t.Fatal("missing create table")
	}
}
