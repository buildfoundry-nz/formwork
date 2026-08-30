//go:build ignore

package shelving

import "context"

// The retired RecordAndUpdateShelvingType / ConfirmShelvingTypes / ShelvingTypePicker
// path is gone; a comment naming it must not trip the gate (decomment-go strips it).
// Classification now lives on the shelving group: paint the mask into a group.
func classify(ctx context.Context, mut *Mutator, mrkID, groupID string) error {
	return mut.RecordAndUpdateShelvingGroup(ctx, mrkID, groupID)
}
