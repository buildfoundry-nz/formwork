package preprocess

import "testing"

func TestLookupRawAndUnknown(t *testing.T) {
	if tr, ok := Lookup("raw"); !ok || tr != nil {
		t.Fatalf(`Lookup("raw") = %v, %v; want nil, true`, tr, ok)
	}
	if tr, ok := Lookup(""); !ok || tr != nil {
		t.Fatalf(`Lookup("") = %v, %v; want nil, true`, tr, ok)
	}
	if _, ok := Lookup("no-such-transform"); ok {
		t.Fatal("unknown transform reported ok")
	}
}

func TestLookupFindsRegisteredTransform(t *testing.T) {
	tr, ok := Lookup("decomment-go")
	if !ok || tr == nil {
		t.Fatalf(`Lookup("decomment-go") = %v, %v; want non-nil, true`, tr, ok)
	}
}

func TestNamesListsAllTransformsSorted(t *testing.T) {
	want := []string{
		"code-only-dart", "comments-only-awk", "comments-only-dart", "comments-only-go", "comments-only-sql",
		"decomment-destring-go", "decomment-go", "decomment-sh", "destring-decomment-sh", "destring-sh",
		"qualify-proto-go-alias", "raw", "strings-only-go", "strings-only-sh",
	}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}
