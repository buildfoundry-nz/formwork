//go:build ignore

// Package main is the service entrypoint.
//
// The //go:build ignore tag above is an artifact of this corpus living inside
// the formwork repository — it keeps the example tree out of formwork's own
// build. Your repo's real source does not need it.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"example.com/shop/internal/handler"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("/orders", handler.Orders)

	logger.Info("listening", "addr", ":8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
