//go:build ignore

package quotes

func insertQuoteRecord(db DB) error {
	const q = `INSERT INTO sample.built_quotes (id) VALUES ($1)` // want: generated-quotes-written-only-by-quotebuild
	return db.Exec(q)
}
