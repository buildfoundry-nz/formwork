//go:build ignore

package detectqueue

import "encoding/json"

// EnqueueIdentification is a thin trampoline into the generic dispatch body — every
// per-kind fact lives on the JobSpec table row, not the arm.
func (c *RemoteQueueEnqueuer) EnqueueIdentification(job IdentificationJob) error {
	return c.enqueueViaTaskQueue(identificationSpec, job)
}

// enqueueViaTaskQueue is the ONE generic body where json.Marshal legitimately
// lives — a single dispatch chokepoint shared by every Enqueue* trampoline.
func (c *RemoteQueueEnqueuer) enqueueViaTaskQueue(spec JobSpec, job any) error {
	body, _ := json.Marshal(job)
	return c.post(body)
}
