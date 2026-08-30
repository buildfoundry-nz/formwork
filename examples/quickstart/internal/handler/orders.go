//go:build ignore

package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"example.com/shop/internal/store"
)

// Orders returns the caller's orders.
//
// This file is the "good" shape all five rules pass over: no print debugging,
// no panic in the request path, and comfortably under the line cap.
func Orders(w http.ResponseWriter, r *http.Request) {
	orders, err := store.ListOrders(r.Context())
	if err != nil {
		slog.Error("list orders", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(orders); err != nil {
		slog.Error("encode orders", "err", err)
	}
}
