// Command formwork evaluates repository guardrail rules defined in
// .formwork/ YAML configuration.
package main

import (
	"os"

	"github.com/buildfoundry-nz/formwork/internal/cli"
	"github.com/buildfoundry-nz/formwork/internal/rules/gotypes"
)

func main() {
	// Under `go vet -vettool`, formwork is the vettool for its own go/types
	// rules, and the go command's arguments are not formwork's CLI.
	if os.Getenv(gotypes.VettoolEnv) == "1" {
		gotypes.Main()
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
