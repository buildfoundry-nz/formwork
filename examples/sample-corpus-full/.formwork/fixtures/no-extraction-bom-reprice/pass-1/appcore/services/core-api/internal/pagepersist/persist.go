//go:build ignore

package pagepersist

import "context"

// SavePage writes one page's rows without any BOM re-price hook.
func SavePage(ctx context.Context, p page) error {
	return store.Save(ctx, p)
}
