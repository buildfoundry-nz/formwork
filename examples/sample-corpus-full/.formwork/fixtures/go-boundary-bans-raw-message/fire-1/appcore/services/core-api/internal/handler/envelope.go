//go:build ignore

package handler

import "encoding/json"

type Envelope struct {
	Body json.RawMessage // want: go-boundary-bans-raw-message
}
