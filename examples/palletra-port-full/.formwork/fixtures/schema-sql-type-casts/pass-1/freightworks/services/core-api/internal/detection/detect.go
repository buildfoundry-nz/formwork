//go:build ignore

package detection

// identifyQuery casts to palletra.item_type — a type the snapshot defines.
const identifyQuery = `
	SELECT id
	FROM palletra.items
	WHERE kind = 'beam'::palletra.item_type`
