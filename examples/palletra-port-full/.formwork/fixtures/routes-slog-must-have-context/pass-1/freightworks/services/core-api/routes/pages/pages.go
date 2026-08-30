//go:build ignore

package pages

import (
	"context"
	"log/slog"
)

func handle(ctx context.Context, err error) {
	slog.ErrorContext(ctx, "pages: load failed", "err", err)
}
