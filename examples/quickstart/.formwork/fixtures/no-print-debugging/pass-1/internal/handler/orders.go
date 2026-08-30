//go:build ignore

package handler

import (
	"log/slog"
	"net/http"
)

func Orders(w http.ResponseWriter, r *http.Request) {
	slog.Info("orders handler reached")
	w.WriteHeader(http.StatusOK)
}
