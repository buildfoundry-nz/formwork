package pattern_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func TestForbiddenPatternEmptyFile(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "pattern: 'anything'\n")
	ms, err := c.CheckFile(scan.NewMemFile("empty.txt", nil))
	if err != nil || len(ms) != 0 {
		t.Fatalf("empty file: matches=%v err=%v", ms, err)
	}
}

func TestRequiredExistsConcurrentCheckFile(t *testing.T) {
	c := mustChecker(t, "required-pattern", "pattern: 'anchor'\nmode: exists\n")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			content := "nothing"
			if i == 7 {
				content = "the anchor"
			}
			if _, err := c.CheckFile(scan.NewMemFile(fmt.Sprintf("f%d.txt", i), []byte(content))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ms := c.(rules.Finalizer).Finalize(); len(ms) != 0 {
		t.Fatalf("anchor was seen concurrently but Finalize fired: %+v", ms)
	}
}
