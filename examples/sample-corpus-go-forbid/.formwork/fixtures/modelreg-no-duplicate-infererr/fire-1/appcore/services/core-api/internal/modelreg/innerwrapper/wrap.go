//go:build ignore

package innerwrapper

import "errors"

// predictErr is a re-introduced duplicate of wrappers.PredictErr.
func predictErr(name string, err error) error { // want: modelreg-no-duplicate-infererr
	return errors.New(name + ": " + err.Error())
}
