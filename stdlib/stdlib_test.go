package stdlib

import (
	"io/fs"
	"strings"
	"testing"
)

func TestOpenGenericReturnsYAML(t *testing.T) {
	fsys, err := Open("generic")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("generic pack embedded zero YAML files — go:embed glob drifted")
	}
}

func TestOpenUnknownNamesTheRoster(t *testing.T) {
	_, err := Open("takeoffqs")
	if err == nil {
		t.Fatal("expected unknown library to fail")
	}
	if !strings.Contains(err.Error(), "unknown library") {
		t.Fatalf("err = %v, want it to say unknown library", err)
	}
	if !strings.Contains(err.Error(), "generic") {
		t.Fatalf("err = %v, want it to list known pack generic", err)
	}
}
