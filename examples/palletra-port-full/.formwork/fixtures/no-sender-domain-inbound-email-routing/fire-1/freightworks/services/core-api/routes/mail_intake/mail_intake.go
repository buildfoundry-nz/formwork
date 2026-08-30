//go:build ignore

package mail_intake

const orgForDomain = `SELECT org_id FROM organizations WHERE email_domain = $1` // want: no-sender-domain-inbound-email-routing
