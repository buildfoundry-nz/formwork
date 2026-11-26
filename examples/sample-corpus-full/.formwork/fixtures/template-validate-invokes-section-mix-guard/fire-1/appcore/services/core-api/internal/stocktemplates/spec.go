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
