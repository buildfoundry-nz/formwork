//go:build ignore

package annotations

import "github.com/palletra/freightworks/services/core-api/internal/markupwrite/mutbase"

func handle() {
	auth := mutbase.ClassificationOverride // want: classification-edit-token-confined-to-owner-files
	_ = auth
}
