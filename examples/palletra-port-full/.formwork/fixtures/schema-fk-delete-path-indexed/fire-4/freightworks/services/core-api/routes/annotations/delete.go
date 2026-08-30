//go:build ignore

package annotations

// DeleteBySection row-deletes annotations on the request path — this seeds the
// #8259 delete-closure with palletra.annotations.
const deleteSQL = `DELETE FROM palletra.annotations WHERE segment_id = $1`
