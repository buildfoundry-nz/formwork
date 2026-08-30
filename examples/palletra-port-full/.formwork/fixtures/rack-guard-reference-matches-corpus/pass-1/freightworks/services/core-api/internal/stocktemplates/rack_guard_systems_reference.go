//go:build ignore

package stocktemplates

const (
	RackGuardDriverPartitionArea = "40.1"
)

// RackGuardReferenceCases is the hand transcription of the corpus evidence.
func RackGuardReferenceCases() []RackGuardReferenceCase {
	return []RackGuardReferenceCase{
		{
			OptionTag:        "ferrostock-guardfilm",
			SampleSwitch:     "corpus switch 41.5",
			Unit:             "m2",
			ShrinkagePercent: "0.00",
			Members: []RackGuardMember{
				member("Ferrostock Sample Wrap", "KIT-A", RackGuardDriverPartitionArea, 0.001),
				member("Sample Edge Tape", "KIT-B", RackGuardDriverPartitionArea, 0.002),
			},
		},
	}
}
