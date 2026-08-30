//go:build ignore

package mutbase

import "google.golang.org/protobuf/encoding/protojson"

// emitRevision routes through the single sanctioned helper — it carries no
// protojson.Marshal token of its own.
func emitRevision(event any) ([]byte, error) {
	return marshalRevisionEvent(event)
}

func marshalRevisionEvent(event any) ([]byte, error) {
	return protojson.Marshal(event)
}

// decodeRevision uses the decode path (Unmarshal), which stays legal.
func decodeRevision(data []byte, event any) error {
	return protojson.Unmarshal(data, event)
}
