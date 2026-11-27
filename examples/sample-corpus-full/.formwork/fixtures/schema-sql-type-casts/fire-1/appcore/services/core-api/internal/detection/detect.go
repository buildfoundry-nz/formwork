//go:build ignore

package detection

// identifyQuery casts to sample.extracted_line_type — a type the schema never
// defines (the real enum is sample.item_type). Compiles, then fails at
// query-plan time in prod (#8097).
const identifyQuery = `
	SELECT id
	FROM sample.items
	WHERE kind = 'unit_spec'::sample.extracted_line_type`
