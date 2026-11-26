//go:build ignore

package main

import "github.com/sample/appcore/internal/coreonly"

func main() {
	_ = coreonly.Value()
}
