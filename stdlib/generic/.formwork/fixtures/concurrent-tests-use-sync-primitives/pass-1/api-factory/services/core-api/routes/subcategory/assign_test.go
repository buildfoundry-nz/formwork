//go:build ignore

package subcategory

import (
	"sync"
	"testing"
)

func TestAssignSubcategoryConcurrentDeleteReturns404(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); handleAssign(t) }()
	go func() { defer wg.Done(); handleDelete(t) }()
	wg.Wait()
}
