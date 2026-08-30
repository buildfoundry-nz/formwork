//go:build ignore

package emailed_drawings

import "context"

// GetUnread reads BOTH tally columns back — every written *_count round-trips
// into the emailed-plans inbox projection.
const selectUnread = `SELECT id, file_count, handled_count FROM palletra.incoming_mail_submissions WHERE viewed = false`

func GetUnread(ctx context.Context, tx Tx) ([]EmailedPlanDelivery, error) {
	return scanUnread(ctx, tx, selectUnread)
}
