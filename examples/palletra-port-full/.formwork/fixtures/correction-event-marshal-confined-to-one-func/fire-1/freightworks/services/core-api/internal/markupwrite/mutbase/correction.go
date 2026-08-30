//go:build ignore

package mutbase

import "google.golang.org/protobuf/encoding/protojson"

// emitRevisionBypass is a NEW emitter that serializes the correction-outbox
// event without routing through the pinned helper — the exact bypass the gate
// forbids.
func emitRevisionBypass(event any) ([]byte, error) {
	return protojson.Marshal(event) // want: correction-event-marshal-confined-to-one-func
}

func marshalRevisionEvent(event any) ([]byte, error) {
	return protojson.Marshal(event)
}
