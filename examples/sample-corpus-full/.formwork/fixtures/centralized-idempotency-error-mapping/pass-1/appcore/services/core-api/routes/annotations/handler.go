//go:build ignore

package annotations

import "github.com/sample/appcore/services/core-api/routes/shared"

func mapErr(err error) {
	shared.WriteCommandError(err)
}
