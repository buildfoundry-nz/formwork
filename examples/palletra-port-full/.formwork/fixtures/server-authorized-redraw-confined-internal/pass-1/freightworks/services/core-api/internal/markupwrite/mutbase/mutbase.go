//go:build ignore

package mutbase

// This is the seam that DEFINES the bypass; it is under markupwrite/ and
// therefore excluded — naming the token here must NOT trip the rule.
var AuthorizedServerRedraw = ModifyAuthorization{bypassLock: true}
