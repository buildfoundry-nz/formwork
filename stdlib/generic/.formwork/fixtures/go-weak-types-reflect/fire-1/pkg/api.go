//go:build ignore

package pkg

import "reflect"

func NameOf(v any) string {
	return reflect.TypeOf(v).String() // want: go-weak-types-reflect
}
