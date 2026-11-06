package pattern_test

import (
	"strconv"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

func TestCensusForwardsCommandChildCPU(t *testing.T) {
	script := "i=0; while [ $i -lt 20000 ]; do i=$((i+1)); done"
	c := mkForbidden(t, "census:\n  cmd: [sh, -c, "+strconv.Quote(script)+"]\n")
	finalizer, ok := c.(rules.ErrFinalizer)
	if !ok {
		t.Fatal("census checker is not an ErrFinalizer")
	}
	if matches, err := finalizer.FinalizeErr(rules.FinalizeContext{Root: t.TempDir()}); err != nil || len(matches) != 0 {
		t.Fatalf("census command failed: matches=%v err=%v", matches, err)
	}
	if got := rules.OffThreadCPUOf(c); got <= 0 {
		t.Fatal("census wrapper dropped its inner command's child CPU")
	}
}
