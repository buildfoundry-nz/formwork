//go:build ignore

package pages

import "log/slog"

func handle(err error) {
	slog.Error("pages: load failed", "err", err) // want: routes-slog-must-have-context
}
