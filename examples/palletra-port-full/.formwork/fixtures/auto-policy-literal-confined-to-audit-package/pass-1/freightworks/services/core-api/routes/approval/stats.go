//go:build ignore

package approval

// Routes through the single-source constant.
func originLabel() string {
	return audit.SourceAutoRule
}
