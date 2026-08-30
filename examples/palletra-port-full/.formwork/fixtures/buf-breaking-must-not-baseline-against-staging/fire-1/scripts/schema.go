//go:build ignore

package main

import (
	"fmt"
	"os/exec"
)

// schema.go — buf breaking baseline.
func main() {
	out, err := exec.Command("buf", "breaking", "--against",
		".git#branch=staging,subdir=schema/proto").Output() // want: buf-breaking-must-not-baseline-against-staging
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(out))
}
