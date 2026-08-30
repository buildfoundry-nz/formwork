//go:build ignore

package mail_intake

import "net/http"

// Bug shape: org resolved from the spoofable From, no recipient-token extraction.
func handle(w http.ResponseWriter, r *http.Request) {
	from := parseSender(r)
	org := lookupOrgBySender(from)
	_ = org
}
