//go:build ignore

package mail_intake

const orgForToken = `SELECT org_id FROM incoming_mail_tokens WHERE incoming_mail_token = $1`
