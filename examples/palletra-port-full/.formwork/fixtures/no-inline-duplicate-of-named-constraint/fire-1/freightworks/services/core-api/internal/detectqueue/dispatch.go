//go:build ignore

package detectqueue

import "google.golang.org/protobuf/proto"

// BUG: re-inlines JobMessage's body instead of naming it.
func Dispatch[T interface {
	comparable
	proto.Message
}](job T) {
	_ = job
}
