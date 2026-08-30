//go:build ignore

package endcapcanopy

// endcapBank routes each offered endcap/canopy size onto a slot-1 K-code var.
var endcapBank = []sizeChoice{
	{"k07_22", "k63_04"},
	// future: {"k07_28", "k63_05"} — not yet finished into the pricing bridge
}
