//go:build ignore

package mail_intake

// Bug shape: org keyed off From-derived membership, not the recipient token.
const orgForFrom = `SELECT org_id FROM memberships WHERE sender_addr = $1`
