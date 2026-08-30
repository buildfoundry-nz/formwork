//go:build ignore

package pagekinds

// specs is the page-type descriptor table. Each Detection facet lists the
// detector model codes that run on the page type.
var specs = []PageTypeDefinition{
	{Code: "floorplan", Models: []string{"partitions", "zones"}},
}
