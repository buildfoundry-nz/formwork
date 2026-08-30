//go:build ignore

package migrations_test

import "testing"

// The seeded-data replay this gate exists to require: migrate to the version
// BEFORE the destructive one, put dependent rows in, then apply it. CI replays
// on an empty database, where DROP COLUMN and SET NOT NULL both succeed
// vacuously.
func TestExtractionFactsCutoverAgainstData(t *testing.T) {
	db := migratePalletraTo(t, "20000318174500")
	execOrFail(t, db, `INSERT INTO palletra.extraction_facts (id, revision_id, legacy_payload)
                     VALUES ($1, $2, $3)`, newID(), newID(), "{}")
	migratePalletraTo(t, "20000402081500")
	assertRowCount(t, db, "palletra.extraction_facts", 1)
}
