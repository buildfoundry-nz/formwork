//go:build ignore

package mail_intake

// Bug shape: org keyed off From-derived membership, not the recipient token.
const orgForFrom = `SELECT org_id FROM memberships WHERE sender_addr = $1`

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// const orgForToken = `SELECT org_id FROM incoming_mail_tokens WHERE incoming_mail_token = $1`
