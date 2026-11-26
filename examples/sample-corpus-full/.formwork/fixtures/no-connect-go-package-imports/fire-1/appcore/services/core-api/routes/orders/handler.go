//go:build ignore

package orders

import (
	"net/http"

	"connectrpc.com/connect" // want: no-connect-go-package-imports
)

func Handler() http.Handler {
	var _ connect.Request[struct{}]
	return nil
}
