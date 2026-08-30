//go:build ignore

package obs

import (
	"log/slog"
	"os"
)

// setup builds a raw slog JSON handler OUTSIDE freightworks/internal/logging, so
// it emits level/msg-shaped logs that GCP Cloud Logging ingests at DEFAULT
// severity — invisible to severity>=ERROR filters and Error Reporting (#1362).
func setup() *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, nil) // want: canonical-structured-logging-handler
	return slog.New(h)
}
