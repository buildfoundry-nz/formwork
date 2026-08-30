//go:build ignore

package runquery

const listActive = `
	SELECT id, name
	FROM palletra.runs
	WHERE status = ANY($1::palletra.run_state[])`
