//go:build ignore

package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// Orders does request decoding, validation, business logic and response
// shaping all in one function. Every step is reasonable on its own; together
// they push the file past the cap, which is exactly the drift file-size is
// meant to notice before it becomes a 300-line handler nobody will touch.
func Orders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	status := strings.TrimSpace(q.Get("status"))
	if status != "" && status != "pending" && status != "shipped" && status != "cancelled" {
		http.Error(w, "unknown status", http.StatusBadRequest)
		return
	}

	limit := 50
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "limit must be a number", http.StatusBadRequest)
			return
		}
		if n < 1 || n > 200 {
			http.Error(w, "limit out of range", http.StatusBadRequest)
			return
		}
		limit = n
	}

	offset := 0
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			http.Error(w, "bad offset", http.StatusBadRequest)
			return
		}
		offset = n
	}

	orders, err := listOrders(r.Context(), status, limit, offset)
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
