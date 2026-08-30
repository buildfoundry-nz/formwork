//go:build ignore

package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Orders is the cured shape: the same behaviour, with request parsing and
// validation moved out. The handler now reads top to bottom in one screen.
func Orders(w http.ResponseWriter, r *http.Request) {
	query, err := parseOrdersQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	orders, err := listOrders(r.Context(), query)
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
