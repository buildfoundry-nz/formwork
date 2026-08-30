//go:build ignore

package detectqueue

import "google.golang.org/protobuf/proto"

// JobMessage is the one home for the dispatchable-job constraint shape.
type JobMessage interface {
	comparable
	proto.Message
}
