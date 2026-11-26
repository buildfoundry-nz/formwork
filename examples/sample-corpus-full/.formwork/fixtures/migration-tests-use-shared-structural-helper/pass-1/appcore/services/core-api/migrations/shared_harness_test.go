//go:build ignore

package migrations

import (
	"os"
	"strings"
	"testing"
)

// loadMigration is the one sanctioned structural read preamble; it sits
// outside the gate's scope, so its os.ReadFile call never trips the check.
func loadMigration(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loadMigration(%s): %v", path, err)
	}
	return string(raw)
}

func lowerSQL(t *testing.T, path string) string {
	sql := loadMigration(t, path)
	return strings.ToLower(sql)
}
