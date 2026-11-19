package positions

import "other"

type localBag = any

type defined any

type fields struct {
	a any             // want `field is any`
	b []any           // want `elem is any`
	c map[string]any  // want `map value is any`
	d other.Bag       // want `field is any`
	e localBag        // want `field is any`
	f defined         // want `field is any`
	g func(any)       // want `param is any`
	h [][]map[any]int // want `map key is any`
	i *any            // want `elem is any`
	j chan any        // want `elem is any`
	k int
}

func param(x any) {} // want `param is any`

func variadic(xs ...any) {} // want `param is any`

func result() any { return nil } // want `result is any`

func wrapped(
	a int,
	b any, // want `param is any`
) {
}

type iface interface {
	m(any) // want `param is any`
}

var pkgVar any // want `var is any`

func inferred() {
	x := result() // want `var is any`
	_ = x
	for _, v := range []any{} { // want `var is any`
		_ = v
	}
}

func generic[T any](x T) T { return x }

type box[T any] struct{ v T }

var boxed box[any] // want `type-arg is any`

func instantiated() {
	_ = generic[any](nil) // want `type-arg is any`
}
