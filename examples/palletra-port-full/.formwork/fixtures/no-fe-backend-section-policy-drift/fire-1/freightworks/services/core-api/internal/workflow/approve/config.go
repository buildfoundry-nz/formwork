//go:build ignore

package approve

// SectionSettings is the server's single source of truth for a section's detector.
type SectionSettings struct {
	SectionTag             string
	IdentificationEndpoint string
}

var externalPartitions = SectionSettings{
	SectionTag:             "external_partitions",
	IdentificationEndpoint: "/api/detection/external-partitions",
}
