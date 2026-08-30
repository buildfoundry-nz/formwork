//go:build ignore

package mail_intake

import (
	"context"
	"log/slog"
)

func handle(ctx context.Context, from, subject string) {
	slog.Info(ctx, "inbound email received", "from", from, "subject", subject) // want: no-raw-sender-or-subject-in-email-logs
}
