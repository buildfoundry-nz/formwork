package rules

// ContentOnly is implemented by a checker whose complete verdict for path can
// be computed from that path and one file's supplied bytes. It is an explicit
// opt-in: implementing Checker alone says nothing about tree independence.
type ContentOnly interface {
	ContentOnly(path string) bool
}

// IsContentOnly reports whether c opts into single-content evaluation for path.
// Unknown checkers default to false.
func IsContentOnly(c Checker, path string) bool {
	co, ok := c.(ContentOnly)
	return ok && co.ContentOnly(path)
}

// CheckerWrapper exposes a wrapped checker for capability discovery. A wrapper
// that forwards CheckFile and ContentOnly but hides a finalizer must expose the
// inner checker here so content-only planning cannot discard that finalizer.
type CheckerWrapper interface {
	UnwrapChecker() Checker
}

// HasFinalizer reports whether c or any explicitly exposed wrapped checker has
// a finalization phase. An excessive wrapper chain is treated as finalizing:
// inability to prove the absence of a whole-tree phase must not admit the rule.
func HasFinalizer(c Checker) bool {
	for depth := 0; depth < 100 && c != nil; depth++ {
		switch c.(type) {
		case ErrFinalizer, Finalizer:
			return true
		}
		w, ok := c.(CheckerWrapper)
		if !ok {
			return false
		}
		c = w.UnwrapChecker()
	}
	return c != nil
}
