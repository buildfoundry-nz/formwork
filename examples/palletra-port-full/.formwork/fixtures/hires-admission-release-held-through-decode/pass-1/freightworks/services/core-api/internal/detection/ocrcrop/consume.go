//go:build ignore

package ocrcrop

import "context"

// consume renders a hi-res crop and defers the admission release across the
// whole decode window (same-scope shape) — the slot is held until return.
func consume(ctx context.Context, page int) error {
	png, w, h, release, err := renderPage(ctx, page)
	if err != nil {
		return err
	}
	defer release()
	return decode(png, w, h)
}
