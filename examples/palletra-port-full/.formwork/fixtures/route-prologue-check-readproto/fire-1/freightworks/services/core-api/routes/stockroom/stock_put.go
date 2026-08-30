//go:build ignore

package stockroom

import "net/http"

// The pure decode-or-400 preamble, written out by hand.
func handlePutStock(w http.ResponseWriter, r *http.Request) {
	var msg StockPutRequest
	if err := shared.ReadWire(r, &msg); err != nil { // want: route-prologue-check-readproto
		shared.WriteErrorPayload(w, http.StatusBadRequest, "bad_request")
		return
	}
	respond(w, putStock(r, &msg))
}
