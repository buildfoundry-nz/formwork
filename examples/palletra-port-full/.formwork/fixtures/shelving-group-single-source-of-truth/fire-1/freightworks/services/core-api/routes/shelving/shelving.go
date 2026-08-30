//go:build ignore

package shelving

import "context"

// A revived per-mask writer: the deleted setter is back in production Go.
func setType(ctx context.Context, mut *Mutator, id, code string) error {
	_, err := mut.RecordAndUpdateShelvingType(ctx, id, code) // want: shelving-group-single-source-of-truth
	return err
}
