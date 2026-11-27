//go:build ignore

package targets

import "context"

type Row struct{ ID string }

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

// ListTargets propagates rows.Err() after the loop, so a mid-stream failure is
// no longer indistinguishable from a clean end-of-rows.
func ListTargets(ctx context.Context, rows Rows) ([]Row, error) {
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
