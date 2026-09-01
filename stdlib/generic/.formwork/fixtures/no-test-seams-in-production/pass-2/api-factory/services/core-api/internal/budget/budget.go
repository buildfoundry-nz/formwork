//go:build ignore
//go:build !production

package budget

import "time"

// perPageBudget is the dev / test seam in the `= identifier` spelling, gated
// behind //go:build !production. The production build compiles the
// //go:build production sibling instead, which declares perPageBudget as a
// plain func over the same signature, so no mutable module-global ships in the
// release binary and every call site stays unconditional.
var perPageBudget = derivePerPageBudget

func derivePerPageBudget(pages int) time.Duration {
	return time.Duration(pages) * time.Second
}

func budgetFor(pages int) time.Duration {
	return perPageBudget(pages)
}
