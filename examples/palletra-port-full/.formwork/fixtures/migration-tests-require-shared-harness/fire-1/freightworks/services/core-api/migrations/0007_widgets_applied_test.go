//go:build ignore

package migrations

import (
	"os"
	"testing"
)

func TestGadgetsMigrationApplied(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL") // want: migration-tests-require-shared-harness
	_ = url
}
