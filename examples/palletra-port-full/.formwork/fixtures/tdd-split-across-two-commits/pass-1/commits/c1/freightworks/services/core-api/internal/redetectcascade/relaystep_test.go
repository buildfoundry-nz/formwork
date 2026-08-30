//go:build ignore

package redetectcascade

import "testing"

func TestCascade(t *testing.T) {
	if Cascade(3) != 6 {
		t.Fatal("want 6")
	}
}
