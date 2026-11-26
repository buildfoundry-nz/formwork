//go:build ignore

package innerwrapper

import "github.com/sample/appcore/services/core-api/internal/modelreg/wrappers"

// Wrap classifies the Visionkit-Infer error via the canonical wrappers helper.
func Wrap(name string, err error) error {
	return wrappers.PredictErr(name, err)
}
