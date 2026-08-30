//go:build ignore

package targets

import "context"

type Row struct{ ID string }

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

// ListTargets drives a rows.Next() loop but never checks rows.Err(): a
// truncated stream reads as a clean end-of-rows and the caller reports success
// with a partial result set (#4854).
func ListTargets(ctx context.Context, rows Rows) ([]Row, error) { // want: go-rows-next-requires-err-check
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
