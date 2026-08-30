//go:build ignore

package v1

import "google.golang.org/protobuf/runtime/protoimpl"

// CreatePartitionResponse acks the create but carries no canonical entity — only a
// membership-id string — so useResourceMutation's cache write silently no-ops.
type CreatePartitionResponse struct { // want: mutation-responses-must-return-entity
	state    protoimpl.MessageState
	RosterId string
}

func (x *CreatePartitionResponse) GetSeatId() string {
	if x != nil {
		return x.RosterId
	}
	return ""
}
