//go:build ignore

package v1

import "google.golang.org/protobuf/runtime/protoimpl"

// CreatePartitionResponse carries the canonical Partition entity, so the FE cache-write
// path receives the fresh row instead of a bare ack.
type CreatePartitionResponse struct {
	state     protoimpl.MessageState
	Partition *v1.Partition
}

func (x *CreatePartitionResponse) GetPartition() *v1.Partition {
	if x != nil {
		return x.Partition
	}
	return nil
}
