//go:build ignore

package foo

import "unsafe"

// launder casts through unsafe.Pointer and silences the linter.
func Bad(p *int) *uint {
	return (*uint)(unsafe.Pointer(p)) //nolint:govet
}
