//go:build ignore

package ocrcrop

import "context"

// consume renders a hi-res crop but never holds the admission release — it
// neither defers it nor stores it on a field, so the weighted slot leaks (#3323).
func consume(ctx context.Context, page int) error {
	png, w, h, release, err := renderPage(ctx, page) // want: hires-admission-release-held-through-decode
	if err != nil {
		return err
	}
	return decode(png, w, h, release)
}
