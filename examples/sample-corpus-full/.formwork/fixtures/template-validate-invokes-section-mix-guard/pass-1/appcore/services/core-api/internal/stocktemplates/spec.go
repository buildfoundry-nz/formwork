//go:build ignore

package stocktemplates

// Validate calls the section-mix guard, passing s.JobType — the required
// anchor is present, so the rule does not fire.
func (s *LayoutSpec) Validate() error {
	for _, sec := range s.Sections {
		if err := ValidateSectionNoNativeKitMix(s.JobType, sec.Key, sec.Items); err != nil {
			return err
		}
	}
	return nil
}
