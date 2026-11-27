//go:build ignore

package gaugebook

// The const-block shape the unscoped port false-fired on: `Category = "floor"`
// matches the pattern, but this file never references the sku catalog, so
// the require_present scope keeps it silent. If require_present ever went
// inert, this fixture fires and the suite catches it.
const CategoryDefault Category = "floor"
