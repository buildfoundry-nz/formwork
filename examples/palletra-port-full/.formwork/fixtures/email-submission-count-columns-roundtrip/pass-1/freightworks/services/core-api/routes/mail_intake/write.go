//go:build ignore

package mail_intake

import "context"

// logSubmission writes BOTH tally columns on the inbound submission.
const insertMailSubmission = `INSERT INTO palletra.incoming_mail_submissions (id, sender_email, file_count, handled_count) VALUES ($1, $2, $3, $4)`

func logSubmission(ctx context.Context, tx Tx, id, from string, attach, done int) error {
	_, err := tx.Exec(ctx, insertMailSubmission, id, from, attach, done)
	return err
}
