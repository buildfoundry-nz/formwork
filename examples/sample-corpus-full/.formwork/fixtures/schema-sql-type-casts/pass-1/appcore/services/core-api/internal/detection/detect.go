//go:build ignore

package detection

// identifyQuery casts to sample.item_type — a type the snapshot defines.
const identifyQuery = `
	SELECT id
	FROM sample.items
	WHERE kind = 'beam'::sample.item_type`
