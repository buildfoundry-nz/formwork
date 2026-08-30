//go:build ignore

package mail_intake

import "net/http"

func handle(w http.ResponseWriter, r *http.Request) {
	tok := inboundaddr.ParseToken(r.Header.Get("To"))
	org := lookupOrgByMailToken(tok)
	_ = org
}
