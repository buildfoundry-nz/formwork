//go:build ignore

package main

import "fmt"

// This containerized binary never wires up the cgroup GOMEMLIMIT call, so it
// runs unbounded and is OOM-killed under cgroup memory pressure (#4870).
func main() {
	fmt.Println("worker starting")
}
