//go:build ignore

package stockroom

import "net/http"

// The same closure, conceded through the classifier: contention SQLSTATEs map
// to a retryable 409 and everything else keeps its 500.
func handleMoveStock(w http.ResponseWriter, r *http.Request) {
	err := db.WithOrg(ctx, ownerRef, func(tx Tx) error {
		return tx.Exec(ctx, moveStockSQL, r.FormValue("bay"))
	})
	if err != nil {
		shared.WriteCommandError(w, err)
		return
	}
	respond(w, nil)
}

// A genuinely non-DB failure keeps its 500 and is legal — but it carries a
// DISTINCT code, never the generic one, so the two are still told apart at the
// client.
func handleRenderStockSheet(w http.ResponseWriter, r *http.Request) {
	sheet, err := renderSheet(ctx, r)
	if err != nil {
		shared.WriteErrorPayload(w, http.StatusInternalServerError, "sheet_render_failed")
		return
	}
	respond(w, sheet)
}
