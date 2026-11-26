//go:build ignore

package pageclassify

import "github.com/sample/appcore/services/core-api/internal/parsecompose/classifier"

func status() string {
	return classifier.ClassificationStatusManuallyConfirmed
}
