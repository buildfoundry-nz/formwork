//go:build ignore

package runquery

const listWaiting = `
	SELECT id, name
	FROM palletra.runs
	WHERE status IN ('pending','running')` // want: run-status-avoid-literals
