//go:build ignore

package skuextract

import (
	"context"

	"github.com/palletra/freightworks/services/core-api/internal/skuentity" // want: grouping-pass-excluded-from-page-lock-packages
)

// Write holds the PAGE lock and then — wrongly — reaches the PROJECT grouping
// pass from inside the write package, nesting the two advisory locks (#2413).
func Write(ctx context.Context, sheetID string) error {
	if _, err := exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", sheetID); err != nil {
		return err
	}
	return skuentity.Group(ctx, sheetID)
}
