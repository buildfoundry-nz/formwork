package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// The calibration must pass on unmutated classifiers. This is the arm that
// turns red when somebody changes a class-2 probe's behaviour, and it is the
// ONLY thing a mutation can bite inside a proof scratch pruned to one rule —
// where the class-2 probes have no content rules left to classify.
func TestClass2SelfTest_PassesOnHealthyClassifiers(t *testing.T) {
	if err := class2SelfTest(); err != nil {
		t.Fatalf("class-2 calibration failed on unmutated classifiers: %v", err)
	}
}

// Built-but-not-called is not built. calibrate() is the function that runs
// before any number this census prints is believed, so the class-2 calibration
// has to be reached from it — otherwise a mutation to a class-2 probe is caught
// by nothing at all, which is the state this file exists to end.
func TestClass2SelfTest_IsWiredIntoCalibrate(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("calibrate.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	i := strings.Index(body, "func calibrate(")
	if i < 0 {
		t.Fatal("calibrate() not found in calibrate.go")
	}
	if !strings.Contains(body[i:], "class2SelfTest()") {
		t.Fatal("calibrate() does not call class2SelfTest() — a calibration nothing invokes " +
			"proves nothing, and inside a pruned proof scratch it is the only thing that can fail")
	}
}

// The calibration is only worth having if it FAILS when a probe stops
// discriminating. Rather than mutate the source, this pins the property the
// cases rest on: the comment plane and the code plane of the same witness must
// give a required-pattern DIFFERENT answers when the token lives only in a
// comment. If that stops holding, COMMENT-SUFFICIENT cannot discriminate and
// the calibration's first case is measuring nothing.
func TestClass2SelfTest_CommentPlaneDiscriminates(t *testing.T) {
	f := scan.NewMemFile("a.go", []byte("// govulncheck runs in CI\npackage a\n"))
	plane, hasComments := commentPlane(f)
	if !hasComments {
		t.Fatal("commentPlane reported no comments in a file whose first line is one")
	}
	pb, err := plane.Content()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(pb, []byte("govulncheck")) {
		t.Fatalf("the comment plane dropped the token it must preserve:\n%s", pb)
	}
	cb, err := codePlane(f).Content()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cb, []byte("govulncheck")) {
		t.Fatalf("the code plane kept a comment-only token, so the two planes do not "+
			"discriminate and COMMENT-SUFFICIENT cannot fire:\n%s", cb)
	}
}
