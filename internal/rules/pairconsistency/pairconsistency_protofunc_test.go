package pairconsistency_test

// where: same-func over PROTO, the grain the validating port needed. The
// unit is one message/enum block, or one rpc body block, bounded by brace
// depth; a service block is a scope, not a unit — the same shape as a Dart
// class — so each rpc body owes its own companion. Units do not nest otherwise: a
// nested message's fields belong to the enclosing message's unit, mirroring
// how the Go mode folds a nested func literal into its enclosing function's
// span. Findings name the unit with its keyword ("message Foo"), because
// "func Foo" would misdescribe a schema block.
//
// Provenance: every firing/erroring test here was RED before the .proto
// dispatch existed. The file-scope option pin was already green and says so.

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

const protoPairParams = "trigger: 'deprecated = true'\nrequires: 'deprecation-anchor'\nwhere: same-func\n"

func TestPairConsistencySameFuncProtoBareMessageFires(t *testing.T) {
	src := `syntax = "proto3";

message LegacyItem {
  string id = 1 [deprecated = true];
}
`
	c := mustChecker(t, protoPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("legacy.proto", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("bare message must fire once, got %+v", ms)
	}
	if ms[0].Line != 4 {
		t.Fatalf("finding must anchor on the trigger line (4), got line %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "message LegacyItem") {
		t.Fatalf("message must name the unit with its keyword, got %q", ms[0].Message)
	}
}

// One anchored message must not buy the anchor for a bare sibling message:
// the file-grain greenwash same-func exists to close.
func TestPairConsistencySameFuncProtoGreenwashMessageDoesNotSatisfy(t *testing.T) {
	src := `syntax = "proto3";

message PricedItem {
  // deprecation-anchor: successor of LegacyItem
  string id = 1;
}

message LegacyItem {
  string id = 1 [deprecated = true];
}
`
	c := mustChecker(t, protoPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("legacy.proto", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("only the bare message may be flagged, got %+v", ms)
	}
	if ms[0].Line != 9 {
		t.Fatalf("finding must anchor on the bare message's trigger line (9), got line %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "message LegacyItem") {
		t.Fatalf("finding must name message LegacyItem, got %q", ms[0].Message)
	}
}

func TestPairConsistencySameFuncProtoAnchoredMessagePasses(t *testing.T) {
	src := `message LegacyItem {
  // deprecation-anchor: superseded by PricedItem
  string id = 1 [deprecated = true];
}
`
	c := mustChecker(t, protoPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("legacy.proto", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("anchor in the same message must pass, got %+v", ms)
	}
}

func TestPairConsistencySameFuncProtoEnumIsAUnit(t *testing.T) {
	src := `enum LegacyKind {
  LEGACY_KIND_UNSPECIFIED = 0 [deprecated = true];
}
`
	c := mustChecker(t, protoPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("legacy.proto", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "enum LegacyKind") {
		t.Fatalf("enum block must be a unit named enum LegacyKind, got %+v", ms)
	}
}

func TestPairConsistencySameFuncProtoRpcBodyIsAUnit(t *testing.T) {
	src := `service LegacyService {
  rpc Get(GetReq) returns (GetResp) {
    option deprecated = true;
  }
}
`
	c := mustChecker(t, protoPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("legacy.proto", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "rpc Get") {
		t.Fatalf("rpc body block must be a unit named rpc Get, got %+v", ms)
	}
}

// Units do not nest: the trigger inside a nested message is judged once, at
// the enclosing message's grain.
func TestPairConsistencySameFuncProtoNestedMessageBelongsToOuter(t *testing.T) {
	src := `message Outer {
  message Inner {
    string id = 1 [deprecated = true];
  }
}
`
	c := mustChecker(t, protoPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("outer.proto", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("nested trigger must fire exactly once, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "message Outer") {
		t.Fatalf("the finding must name the OUTER unit, got %q", ms[0].Message)
	}
}

func TestPairConsistencySameFuncProtoUnterminatedBlockIsError(t *testing.T) {
	c := mustChecker(t, protoPairParams)
	_, err := c.CheckFile(scan.NewMemFile("broken.proto", []byte("message Broken {\n  string id = 1 [deprecated = true];\n")))
	if err == nil {
		t.Fatal("unterminated message block must be an error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "broken.proto") {
		t.Fatalf("error must name the file, got %v", err)
	}
}

// (green from RED) Pin: a file-level option belongs to no unit, so a trigger
// there is unjudged — the proto mode's disclosed residue, mirroring the Go
// mode's package-level initializer exclusion.
func TestPairConsistencySameFuncProtoFileScopeOptionHasNoUnit(t *testing.T) {
	c := mustChecker(t, protoPairParams)
	ms, err := c.CheckFile(scan.NewMemFile("fileopts.proto", []byte("syntax = \"proto3\";\n\noption deprecated = true;\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("file-scope option must stay un-united, got %+v", ms)
	}
}
