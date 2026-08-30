//go:build ignore

package partitionheights

// A legit page_id filter in a file that never builds the SourceTable read — the
// false positive the require_present scope exists to suppress (the original
// port false-fired on exactly this file shape). If require_present ever went
// inert, this fixture fires and the suite catches it.
const clusterQuery = "SELECT id FROM palletra.partition_height_groups am WHERE am.page_id = $1 AND am.project_id = $2"
