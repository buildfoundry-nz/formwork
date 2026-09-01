//go:build ignore

package budget

import "time"

// perPageBudget is an ungated module-global test seam written in the
// `= identifier` spelling rather than the `Hook`-named function-typed one: the
// var aliases the real deriver, and a unit test swaps it for a stub that records
// the call sequence. It carries no //go:build tag, so the mutable global ships
// in the production binary exactly as a `Hook`-named seam would. Neither the
// name nor the declared type is what makes it a seam — the mutable
// package-level var is (audit-17 #14 / #9874).
var perPageBudget = derivePerPageBudget // want: no-test-seams-in-production

func derivePerPageBudget(pages int) time.Duration {
	return time.Duration(pages) * time.Second
}

func budgetFor(pages int) time.Duration {
	return perPageBudget(pages)
}
