//go:build ignore

package emailed_drawings

import "context"

// GetUnread projects only the attachment tally — the processed tally is
// written by the inbound pipeline but never read back (the #8860 write-only
// shadow). Comments are stripped before matching, so this mention does not count.
const selectUnread = `SELECT id, file_count FROM palletra.incoming_mail_submissions WHERE viewed = false`

func GetUnread(ctx context.Context, tx Tx) ([]EmailedPlanDelivery, error) {
	return scanUnread(ctx, tx, selectUnread)
}
