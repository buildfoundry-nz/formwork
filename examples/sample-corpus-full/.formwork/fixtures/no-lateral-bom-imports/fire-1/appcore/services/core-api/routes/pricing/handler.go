//go:build ignore

package pricing

import "github.com/sample/appcore/services/core-api/routes/bom" // want: no-lateral-bom-imports

func Handle() { _ = bom.Nil }
