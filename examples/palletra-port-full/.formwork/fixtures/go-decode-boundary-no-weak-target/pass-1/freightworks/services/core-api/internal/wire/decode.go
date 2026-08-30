//go:build ignore

package wire

import "encoding/json"

type Payload struct {
	Name string `json:"name"`
}

func decode(data []byte) (Payload, error) {
	var p Payload
	err := json.Unmarshal(data, &p)
	return p, err
}
