//go:build ignore

package main

import "github.com/sample/appcore/internal/logging"

func main() {
	logging.Init("core-api")
	start()
}
