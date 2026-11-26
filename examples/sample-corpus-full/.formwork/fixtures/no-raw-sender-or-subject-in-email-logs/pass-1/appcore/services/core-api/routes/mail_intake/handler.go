//go:build ignore

package mail_intake

import (
	"context"
	"log/slog"
)

func handle(ctx context.Context, from, subject, emailID string) {
	// Log the sender DOMAIN and the provider emailId only — never raw PII.
	slog.Info(ctx, "inbound email received", "redactedDomain", mailerDomain(from), "emailId", emailID)
}
