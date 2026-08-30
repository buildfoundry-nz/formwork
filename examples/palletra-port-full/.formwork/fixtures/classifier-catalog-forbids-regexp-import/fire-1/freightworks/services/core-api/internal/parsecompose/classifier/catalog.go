//go:build ignore

package classifier

import (
	"regexp" // want: classifier-catalog-forbids-regexp-import
)

var titleRE = regexp.MustCompile(`^page`)
