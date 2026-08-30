//go:build ignore

package skusidebar

// A qualified READ of the column (bli.project_sku_id) — allowed everywhere.
func querySQL() string {
	return "SELECT bli.project_sku_id FROM palletra.bom_line_items bli WHERE bli.project_id = $1"
}
