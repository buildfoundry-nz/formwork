//go:build ignore

package geminiuse

import "example.com/api/internal/gemini"

func f() {
	_ = gemini.CallTag{} // want: gemini-call-label-never-zero-value
}
