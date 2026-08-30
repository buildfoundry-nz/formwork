//go:build ignore

package kitimport

// declaredOptions lists the FE-offered class switches. The endcap range
// spans k07_22..k07_28 (documented here in prose only).
var declaredOptions = []declaredOption{
	{Code: "PLT.EndCap.None", Var: "k07_22", Category: declEndcap},
	{Code: "PLT.Canopy.Wide", Var: "k07_28", Category: declOverhang},
	{Code: "PLT.BaseRail.Standard", Var: "k18_41", Category: declBaseRail},
}
