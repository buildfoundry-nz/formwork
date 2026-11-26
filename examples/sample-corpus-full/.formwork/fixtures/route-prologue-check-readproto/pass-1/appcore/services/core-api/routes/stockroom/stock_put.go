//go:build ignore

package stockroom

import "net/http"

// The collapsed form.
func handlePutStock(w http.ResponseWriter, r *http.Request) {
	var msg StockPutRequest
	if !shared.ParseOr400(w, r, &msg) {
		return
	}
	respond(w, putStock(r, &msg))
}

// A COMPOUND decode keeps ReadWireBody directly: the extra condition on the
// same line is what the pattern's no-pipe carve-out is for, because collapsing
// it would drop the validation half.
func handlePatchStock(w http.ResponseWriter, r *http.Request) {
	var msg StockPatchRequest
	if err := shared.ReadWireBody(r, &msg); err != nil || msg.GetStockId() == "" {
		shared.WriteErrorPayload(w, http.StatusBadRequest, "bad_request")
		return
	}
	respond(w, patchStock(r, &msg))
}
