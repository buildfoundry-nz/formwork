//go:build ignore

package wire

import "encoding/json"

func decode(data []byte) {
	_ = json.Unmarshal(data, &map[string]any{}) // want: go-decode-boundary-no-weak-target
}
