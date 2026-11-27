package onlyparams

type fields struct {
	a any
	b []any
}

func param(x any) {} // want `param is any`

func result() any { return nil }

var pkgVar any
