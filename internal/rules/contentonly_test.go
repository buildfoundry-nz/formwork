package rules_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type pathContentChecker struct{ path string }

func (c pathContentChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (c pathContentChecker) ContentOnly(path string) bool                { return path == c.path }

type finalizingChecker struct{ errFinalizer bool }

func (c finalizingChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (c finalizingChecker) Finalize() []rules.Match                     { return nil }
func (c finalizingChecker) FinalizeErr(rules.FinalizeContext) ([]rules.Match, error) {
	return nil, nil
}

// contentWrapper deliberately hides the inner finalizer methods. ContentOnly
// alone must not make that wrapper safe: its wrapper capability exposes the
// hidden finalizer to the planner.
type contentWrapper struct{ inner rules.Checker }

func (w contentWrapper) CheckFile(f *scan.File) ([]rules.Match, error) {
	return w.inner.CheckFile(f)
}
func (w contentWrapper) ContentOnly(string) bool      { return true }
func (w contentWrapper) UnwrapChecker() rules.Checker { return w.inner }

func TestContentOnlyIsPathAwareAndDefaultsFalse(t *testing.T) {
	if rules.IsContentOnly(nopChecker{}, "a.sql") {
		t.Fatal("an unknown checker must not be content-only by default")
	}
	c := pathContentChecker{path: "a.sql"}
	if !rules.IsContentOnly(c, "a.sql") {
		t.Fatal("explicit path opt-in was not honored")
	}
	if rules.IsContentOnly(c, "a.go") {
		t.Fatal("opt-in for one path leaked to another")
	}
}

func TestHasFinalizerSeesThroughContentWrapper(t *testing.T) {
	for _, inner := range []rules.Checker{
		finalizingChecker{},
		wrappedErrOnly{},
	} {
		wrapped := contentWrapper{inner: inner}
		if !rules.IsContentOnly(wrapped, "a.txt") {
			t.Fatal("test wrapper must opt into content evaluation")
		}
		if !rules.HasFinalizer(wrapped) {
			t.Fatalf("wrapped finalizer capability hidden for %T", inner)
		}
	}
	if rules.HasFinalizer(contentWrapper{inner: nopChecker{}}) {
		t.Fatal("plain wrapped checker reported a finalizer")
	}
}

type wrappedErrOnly struct{}

func (wrappedErrOnly) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (wrappedErrOnly) FinalizeErr(rules.FinalizeContext) ([]rules.Match, error) {
	return nil, nil
}
