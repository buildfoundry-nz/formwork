//go:build ignore

package fieldcheck

import "regexp"

// plt-validation-rule:email
var emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// plt-validation-rule:phone
var phoneRE = regexp.MustCompile(`^\+?[0-9]{7,15}$`)

func IsWellFormedEmail(s string) bool { return emailRE.MatchString(s) }
func IsWellFormedPhone(s string) bool { return phoneRE.MatchString(s) }
