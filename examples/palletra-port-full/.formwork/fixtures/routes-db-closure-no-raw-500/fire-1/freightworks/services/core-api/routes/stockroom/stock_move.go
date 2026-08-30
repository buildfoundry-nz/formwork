//go:build ignore

package stockroom

import "net/http"

// The regression this gate stops: the handler opens a db.WithOrg closure and
// then concedes that closure's error as a generic 500, so a 55P03 lock wait
// reaches the client as a permanent internal error instead of a retryable 409.
func handleMoveStock(w http.ResponseWriter, r *http.Request) {
	err := db.WithOrg(ctx, ownerRef, func(tx Tx) error { // want: routes-db-closure-no-raw-500
		return tx.Exec(ctx, moveStockSQL, r.FormValue("bay"))
	})
	if err != nil {
		shared.WriteErrorPayload(w, http.StatusInternalServerError, "internal_error")
		return
	}
	respond(w, nil)
}
