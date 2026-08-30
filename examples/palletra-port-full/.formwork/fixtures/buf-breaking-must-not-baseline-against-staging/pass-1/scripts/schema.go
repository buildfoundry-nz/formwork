//go:build ignore

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// schema.go — buf breaking baseline at the develop merge-base.
func main() {
	base, err := exec.Command("git", "merge-base", "origin/develop", "HEAD").Output()
	if err != nil {
		fmt.Println(err)
		return
	}
	against := ".git#ref=" + strings.TrimSpace(string(base)) + ",subdir=schema/proto"
	out, err := exec.Command("buf", "breaking", "--against", against).Output()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(out))
}
