//go:build ignore

package detectqueue

import "encoding/json"

// EnqueueIdentification open-codes the marshal+dispatch instead of trampolining into
// the generic enqueueViaTaskQueue body — the sweep-1 #10 hand-rolled fork
// shape. json.Marshal belongs in the generic body, not a per-kind arm.
func (c *RemoteQueueEnqueuer) EnqueueIdentification(job IdentificationJob) error {
	body, _ := json.Marshal(job) // want: go-job-marshal-confined-to-dispatch-body
	return c.post(body)
}
