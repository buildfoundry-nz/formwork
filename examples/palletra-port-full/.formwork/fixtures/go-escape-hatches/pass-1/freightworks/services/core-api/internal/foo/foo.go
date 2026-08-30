//go:build ignore

package foo

// Good returns a typed value with no escape hatches.
func Good(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
