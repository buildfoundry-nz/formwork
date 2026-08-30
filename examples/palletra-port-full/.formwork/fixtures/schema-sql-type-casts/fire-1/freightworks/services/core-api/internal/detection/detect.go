//go:build ignore

package detection

// identifyQuery casts to palletra.extracted_line_type — a type the schema never
// defines (the real enum is palletra.item_type). Compiles, then fails at
// query-plan time in prod (#8097).
const identifyQuery = `
	SELECT id
	FROM palletra.items
	WHERE kind = 'unit_spec'::palletra.extracted_line_type`
