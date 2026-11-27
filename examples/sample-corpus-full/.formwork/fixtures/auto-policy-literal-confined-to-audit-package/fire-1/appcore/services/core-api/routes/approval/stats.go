//go:build ignore

package approval

// Re-open-codes the metadata.source discriminator instead of using audit.SourceAutoRule.
func originLabel() string {
	return "auto_rule" // want: auto-policy-literal-confined-to-audit-package
}
