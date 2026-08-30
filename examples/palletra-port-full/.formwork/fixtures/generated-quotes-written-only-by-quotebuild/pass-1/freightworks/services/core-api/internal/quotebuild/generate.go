//go:build ignore

package quotebuild

func Generate(db DB) error {
	const q = `INSERT INTO palletra.built_quotes (id) VALUES ($1)`
	return db.Exec(q)
}
