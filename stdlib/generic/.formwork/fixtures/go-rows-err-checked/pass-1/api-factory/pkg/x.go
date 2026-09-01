package pkg

func Load(rows interface{ Next() bool; Err() error }) error {
	for rows.Next() {
	}
	return rows.Err()
}
