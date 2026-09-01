//go:build ignore

package wire

import "encoding/json"

// Under *.pb.go exclude — must not fire. Re-spelling exclude to *_pb.go reddens.
func decode(data []byte) {
	_ = json.Unmarshal(data, &map[string]any{})
}