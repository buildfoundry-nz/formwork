//go:build ignore

package handler

import (
	"fmt"
	"net/http"
)

func Orders(w http.ResponseWriter, r *http.Request) {
	fmt.Println("orders handler reached") // want: no-print-debugging
	w.WriteHeader(http.StatusOK)
}
