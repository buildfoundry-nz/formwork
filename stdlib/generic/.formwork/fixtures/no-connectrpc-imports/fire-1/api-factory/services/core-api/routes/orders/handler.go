//go:build ignore

package orders

import (
	"net/http"

	"connectrpc.com/connect" // want: no-connectrpc-imports
)

func Handler() http.Handler {
	var _ connect.Request[struct{}]
	return nil
}
