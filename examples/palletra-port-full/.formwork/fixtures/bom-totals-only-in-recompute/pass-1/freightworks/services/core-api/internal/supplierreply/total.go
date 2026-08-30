//go:build ignore

package supplierreply

// A supplier's own quote total lives on supplier_replies, not boms. The
// table-aware gate keys on the write target, so this write is out of scope.
func setSupplierAmount() string {
	return "UPDATE palletra.supplier_replies SET total_ex_vat = $1 WHERE id = $2"
}
