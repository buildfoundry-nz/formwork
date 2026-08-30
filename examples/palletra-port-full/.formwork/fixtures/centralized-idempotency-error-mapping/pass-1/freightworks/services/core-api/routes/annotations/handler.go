//go:build ignore

package annotations

import "github.com/palletra/freightworks/services/core-api/routes/shared"

func mapErr(err error) {
	shared.WriteCommandError(err)
}
