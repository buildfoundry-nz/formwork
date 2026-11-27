//go:build ignore

package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestPagesMigrationLayout(t *testing.T) {
	// A re-pasted structural read preamble instead of the shared helper.
	data, err := os.ReadFile("0042_pages.sql") // want: migration-tests-use-shared-structural-helper
	if err != nil {
		t.Fatalf("read up: %v", err)
	}
	sql := strings.ToLower(string(data))
	if !strings.Contains(sql, "create table") {
		t.Fatal("missing create table")
	}
}
