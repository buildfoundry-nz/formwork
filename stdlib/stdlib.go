// Package stdlib is the versioned generic rule packs shipped with the binary.
//
// Adopters opt in from .formwork/formwork.yaml:
//
//	library: [generic]
//
// Pack YAML lives on disk under stdlib/<name>/.formwork/rules/ so
// `formwork test -C stdlib/<name>` proves the pack, and is embedded so a
// pinned binary cannot drift from the pack it claims to ship.
package stdlib

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed generic/.formwork/rules/*.yaml
var genericRules embed.FS

// Open returns the rule-file filesystem for a named pack (YAML files at the
// FS root). Unknown names are a config error, not a silent empty pack.
func Open(name string) (fs.FS, error) {
	switch name {
	case "generic":
		return fs.Sub(genericRules, "generic/.formwork/rules")
	default:
		return nil, fmt.Errorf("unknown library %q (known: %s)", name, strings.Join(Names(), ", "))
	}
}

// Names is the pack roster, sorted. Load quotes this on an unknown name so
// the operator does not have to grep the binary.
func Names() []string {
	out := []string{"generic"}
	sort.Strings(out)
	return out
}
