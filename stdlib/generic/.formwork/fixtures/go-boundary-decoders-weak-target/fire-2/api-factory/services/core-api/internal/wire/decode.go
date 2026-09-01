//go:build ignore

package wire

import j "encoding/json"

func decode(data []byte) {
	_ = j.Unmarshal(data, &map[string]any{}) // want: go-boundary-decoders-weak-target
}
