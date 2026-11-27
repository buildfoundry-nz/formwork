//go:build ignore

package runquery

const listActive = `
	SELECT id, name
	FROM sample.runs
	WHERE status = ANY($1::sample.run_state[])`
