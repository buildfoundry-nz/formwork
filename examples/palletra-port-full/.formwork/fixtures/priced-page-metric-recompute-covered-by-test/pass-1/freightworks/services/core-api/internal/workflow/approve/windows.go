//go:build ignore

package approve

// The windows approve section prices endcap_count: on approve it must
// materialize into palletra.page_gauges via pagerefresh.Recompute.
var Sections = []Section{
	{
		Code: "windows",
		PageTallyCodes: []string{
			"endcap_count",
		},
	},
}
