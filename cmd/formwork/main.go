// Command formwork evaluates repository guardrail rules defined in
// .formwork/ YAML configuration.
package main

import (
	"os"

	"github.com/buildfoundry-nz/formwork/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
