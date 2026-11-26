//go:build ignore

package types

type Shape struct {
	Points   []Point
	shapeTag string // want: no-inert-shape-discriminator-tag
}
