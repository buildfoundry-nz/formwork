//go:build ignore

package handler

import "encoding/json"

// Under *.pb.go exclude — must not fire. Re-spelling exclude to *_pb.go reddens.
type Envelope struct {
	Body json.RawMessage
}
