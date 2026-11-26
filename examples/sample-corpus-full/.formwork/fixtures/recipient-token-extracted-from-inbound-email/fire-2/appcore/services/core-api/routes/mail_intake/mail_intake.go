//go:build ignore

package mail_intake

import "net/http"

// Bug shape: org resolved from the spoofable From, no recipient-token extraction.
func handle(w http.ResponseWriter, r *http.Request) {
	from := parseSender(r)
	org := lookupOrgBySender(from)
	_ = org
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// tok := inboundaddr.ParseToken(r.Header.Get("To"))
