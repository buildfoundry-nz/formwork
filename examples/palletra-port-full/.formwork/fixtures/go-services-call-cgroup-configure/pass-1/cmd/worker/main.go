//go:build ignore

package main

import (
	"fmt"

	"github.com/palletra/freightworks/internal/runtimeenv"
)

// Configure() sets GOMEMLIMIT from the cgroup memory limit before any work, so
// the process GCs under pressure instead of being OOM-killed (#1148).
func main() {
	runtimeenv.Configure()
	fmt.Println("worker starting")
}
