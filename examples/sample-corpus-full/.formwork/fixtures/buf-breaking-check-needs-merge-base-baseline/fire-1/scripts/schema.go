//go:build ignore

package main

import (
	"fmt"
	"os/exec"
)

// schema.go — buf breaking baseline against the moving branch tip.
func main() {
	out, err := exec.Command("buf", "breaking", "--against", "origin/develop").Output()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(out))
}
