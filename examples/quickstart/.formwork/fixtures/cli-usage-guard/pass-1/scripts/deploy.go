//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: deploy <image-tag>")
		os.Exit(2)
	}
	tag := os.Args[1]

	fmt.Println("deploying", tag)
	run("kubectl", "set", "image", "deployment/api", "api=registry.example.com/api:"+tag)
	run("kubectl", "rollout", "status", "deployment/api", "--timeout=120s")
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
