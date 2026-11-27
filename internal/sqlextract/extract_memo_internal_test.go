package sqlextract

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func TestMemoExtractionBuildsOnceForConcurrentReaders(t *testing.T) {
	src := []byte("package db\n")
	fs := &scan.FileSet{Files: []*scan.File{scan.NewMemFile("q.go", src)}}
	var calls atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cands, _, err := memoExtraction(fs, "q.go", src, false, func() ([]Candidate, []Site, error) {
				calls.Add(1)
				return []Candidate{{Text: "SELECT id FROM things"}}, nil, nil
			})
			if err != nil || len(cands) != 1 || cands[0].Text != "SELECT id FROM things" {
				t.Errorf("candidates=%+v err=%v", cands, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("built extraction %d times for one walk/file/mode, want 1", got)
	}
}

func TestMemoExtractionKeepsModesAndErrors(t *testing.T) {
	src := []byte("package db\n")
	fs := &scan.FileSet{Files: []*scan.File{scan.NewMemFile("q.go", src)}}
	broken := errors.New("distinctive parse failure")
	for _, mode := range []bool{false, true} {
		calls := 0
		for range 2 {
			_, _, err := memoExtraction(fs, "q.go", src, mode, func() ([]Candidate, []Site, error) {
				calls++
				return nil, nil, broken
			})
			if !errors.Is(err, broken) {
				t.Fatalf("reassembled=%t lost parse error: %v", mode, err)
			}
		}
		if calls != 1 {
			t.Fatalf("reassembled=%t built %d times, want 1", mode, calls)
		}
	}
}

func TestMemoExtractionDoesNotRetainVariantOrUnwalkedSources(t *testing.T) {
	src := []byte("package db\n")
	fs := &scan.FileSet{Files: []*scan.File{scan.NewMemFile("q.go", src)}}
	for _, tc := range []struct {
		name string
		tree *scan.FileSet
		path string
		src  []byte
	}{
		{"no walk", nil, "q.go", src},
		{"outside walk", fs, "other.go", src},
		{"unreadable canonical", &scan.FileSet{Files: []*scan.File{new(scan.File)}}, "", src},
		{"masked source", fs, "q.go", []byte("package db\n// masked\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			for range 2 {
				_, _, err := memoExtraction(tc.tree, tc.path, tc.src, false, func() ([]Candidate, []Site, error) {
					calls++
					return nil, nil, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2 {
				t.Fatalf("retained an unbounded variant source: built %d times, want 2", calls)
			}
		})
	}
}
