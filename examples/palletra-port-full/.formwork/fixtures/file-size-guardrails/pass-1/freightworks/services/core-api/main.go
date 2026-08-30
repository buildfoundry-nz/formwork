//go:build ignore

package main

import "github.com/palletra/freightworks/internal/logging"

func main() {
	logging.Init("core-api")
	start()
}
