//go:build ignore

package handler

import "reflect"

func sameShape(a, b string) bool {
	return reflect.DeepEqual(a, b) // want: go-app-code-bans-reflect
}
