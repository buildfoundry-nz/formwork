//go:build ignore

package pageclassify

import "github.com/palletra/freightworks/services/core-api/internal/parsecompose/classifier"

func status() string {
	return classifier.ClassificationStatusManuallyConfirmed
}
