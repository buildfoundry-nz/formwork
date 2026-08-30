//go:build ignore

package stocktemplates

// The section-mix guard call was removed: Validate no longer calls
// ValidateSectionNoNativeKitMix with s.JobType, so a native + Kit-formula
// section can slip through. The required anchor is absent, so the rule fires.
func (s *LayoutSpec) Validate() error {
	for _, sec := range s.Sections {
		if err := validateSectionKey(sec.Key); err != nil {
			return err
		}
	}
	return nil
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// if err := ValidateSectionNoNativeKitMix(s.JobType, sec.Key, sec.Items); err != nil {
