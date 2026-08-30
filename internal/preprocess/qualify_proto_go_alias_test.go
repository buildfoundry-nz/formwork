package preprocess

import (
	"strings"
	"testing"
)

// The proto fixtures below are deliberately fictional — the repo's placeholder
// product, on the reserved example.com domain. A realistic-looking one pasted
// from the validating target's own schema is a leak rather than a better
// fixture: it publishes that target's proto namespace and its owned Go-module
// domain through the OSS cut, which is how #321 arrived. The standing catch is
// the real-cut arm in internal/publication/drift_test.go, not this comment.
func TestQualifyProtoGoAlias(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"rewrites top-level message with go_package alias": {
			in: "syntax = \"proto3\";\n" +
				"package palletra.domain.v1;\n" +
				"option go_package = \"example.com/palletra/gen/go/palletra/domain/v1;domainv1\";\n" +
				"message Foo {\n" +
				"  string x = 1;\n" +
				"}\n",
			want: "syntax = \"proto3\";\n" +
				"package palletra.domain.v1;\n" +
				"option go_package = \"example.com/palletra/gen/go/palletra/domain/v1;domainv1\";\n" +
				"message domainv1.Foo {\n" +
				"  string x = 1;\n" +
				"}\n",
		},
		"nested indented message stays bare": {
			in: "option go_package = \"p;apiv1\";\n" +
				"message Outer {\n" +
				"  message Inner {\n" +
				"  }\n" +
				"}\n",
			want: "option go_package = \"p;apiv1\";\n" +
				"message apiv1.Outer {\n" +
				"  message Inner {\n" +
				"  }\n" +
				"}\n",
		},
		"no go_package leaves messages unchanged": {
			in:   "package palletra.api.v1;\nmessage Foo {\n}\n",
			want: "package palletra.api.v1;\nmessage Foo {\n}\n",
		},
		"go_package without semicolon alias is unchanged": {
			in:   "option go_package = \"example.com/palletra/gen/go/palletra/api/v1\";\nmessage Foo {\n}\n",
			want: "option go_package = \"example.com/palletra/gen/go/palletra/api/v1\";\nmessage Foo {\n}\n",
		},
		"brace-on-next-line is not rewritten": {
			in:   "option go_package = \"p;domainv1\";\nmessage Foo\n{\n}\n",
			want: "option go_package = \"p;domainv1\";\nmessage Foo\n{\n}\n",
		},
		"preserves crlf": {
			in:   "option go_package = \"p;evalv1\";\r\nmessage Bar {\r\n}\r\n",
			want: "option go_package = \"p;evalv1\";\r\nmessage evalv1.Bar {\r\n}\r\n",
		},
		"empty input": {in: "", want: ""},
		"message with no space before brace": {
			in:   "option go_package = \"p;commonv1\";\nmessage Baz{\n}\n",
			want: "option go_package = \"p;commonv1\";\nmessage commonv1.Baz{\n}\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(QualifyProtoGoAlias([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatalf("line count changed: %d -> %d", strings.Count(c.in, "\n"), strings.Count(got, "\n"))
			}
		})
	}
}

func TestQualifyProtoGoAliasDoesNotMutateInput(t *testing.T) {
	in := []byte("option go_package = \"p;domainv1\";\nmessage Foo {\n}\n")
	orig := string(in)
	_ = QualifyProtoGoAlias(in)
	if string(in) != orig {
		t.Fatal("input mutated")
	}
}

// TestQualifyProtoGoAliasReadsOnlyRealOptions pins #267.5. The alias scan used
// to run over raw bytes and keep the LAST valid option it found, so a
// commented-out historical `option go_package` — exactly what a proto carries
// mid-migration — overrode the live one and every message in the file came out
// qualified with a dead alias. The cure has two halves and both are pinned
// here: comments are masked out before the scan, and the FIRST option the file
// declares is the one that decides.
//
// The mask covers comments ONLY. A go_package path is a URL-shaped string and
// may carry `//` inside its quotes; blanking string contents as well would
// erase the alias the transform exists to read, so the third case below fails
// if the mask ever widens to strings.
func TestQualifyProtoGoAliasReadsOnlyRealOptions(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"block-commented-out option after the real one is not the alias": {
			in: "syntax = \"proto3\";\n" +
				"option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"*/\n" +
				"message Foo {\n" +
				"}\n",
			want: "syntax = \"proto3\";\n" +
				"option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"*/\n" +
				"message domainv1.Foo {\n" +
				"}\n",
		},
		"line-commented-out option after the real one is not the alias": {
			in: "option go_package = \"example.com/gen/domain/v1;domainv1\"; // live\n" +
				"// option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message Foo {\n" +
				"}\n",
			want: "option go_package = \"example.com/gen/domain/v1;domainv1\"; // live\n" +
				"// option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message domainv1.Foo {\n" +
				"}\n",
		},
		"a // inside the quoted path is part of the path, not a comment": {
			in: "option go_package = \"example.com//gen/domain/v1;domainv1\";\n" +
				"message Foo {\n" +
				"}\n",
			want: "option go_package = \"example.com//gen/domain/v1;domainv1\";\n" +
				"message domainv1.Foo {\n" +
				"}\n",
		},
		"the first real option wins over a second real one": {
			in: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message Foo {\n" +
				"}\n",
			want: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message domainv1.Foo {\n" +
				"}\n",
		},
		"a first option whose alias is not an identifier is no alias": {
			in: "option go_package = \"example.com/gen/domain/v1;domain-v1\";\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message Foo {\n" +
				"}\n",
			want: "option go_package = \"example.com/gen/domain/v1;domain-v1\";\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message Foo {\n" +
				"}\n",
		},
		"a first option carrying no alias is not rescued by a later one": {
			in: "option go_package = \"example.com/gen/domain/v1\";\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message Foo {\n" +
				"}\n",
			want: "option go_package = \"example.com/gen/domain/v1\";\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"message Foo {\n" +
				"}\n",
		},
		// Proto has no raw-string form, so a backtick reaching code position
		// means the file was mangled — a line pasted back in with its markdown
		// quoting, most often. Under Go's backtick rule the lone backtick below
		// opens a raw string that runs to EOF, the /* */ after it stops being a
		// comment, and the mask silently stops masking: the dead ghost option
		// becomes the first one the scan sees. The proto dialect reads the
		// backtick as ordinary code, so the block comment is still a comment.
		"a stray backtick is code, not a raw string that hides the rest of the file": {
			in: "syntax = \"proto3\";\n" +
				"`\n" +
				"/*\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"*/\n" +
				"option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"message Foo {\n" +
				"}\n",
			want: "syntax = \"proto3\";\n" +
				"`\n" +
				"/*\n" +
				"option go_package = \"example.com/gen/ghost/v1;ghostv1\";\n" +
				"*/\n" +
				"option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"message domainv1.Foo {\n" +
				"}\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(QualifyProtoGoAlias([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatalf("line count changed: %d -> %d", strings.Count(c.in, "\n"), strings.Count(got, "\n"))
			}
		})
	}
}

// TestQualifyProtoGoAliasQualifiesOnlyRealMessages pins #267.6. The alias scan
// was taught to read a comment-masked copy (#267.5), but the rewrite loop kept
// running topLevelMessageRe over the raw line, so a column-0 `message X {`
// sitting inside a /* */ block — a message under revision, or one kept for
// reference — came out qualified and a phantom entered the stream. Masking is
// the cure for both halves: match against the mask, emit from the original.
//
// The cases below pin that shape from both sides. Masking too little brings the
// phantom back. Masking too much — emitting the masked bytes, or letting the
// mask reach past the block's `*/` — destroys a real message's own comments or
// swallows the messages after the block, so the guards fail too.
func TestQualifyProtoGoAliasQualifiesOnlyRealMessages(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"a column-0 message inside a block comment stays bare": {
			in: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"message Ghost {\n" +
				"}\n" +
				"*/\n",
			want: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"message Ghost {\n" +
				"}\n" +
				"*/\n",
		},
		"an indented message inside a block comment stays bare": {
			in: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"  message Ghost {\n" +
				"  }\n" +
				"*/\n",
			want: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"  message Ghost {\n" +
				"  }\n" +
				"*/\n",
		},
		"a real message after the block closes is still qualified": {
			in: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"message Ghost {\n" +
				"}\n" +
				"*/\n" +
				"message Real {\n" +
				"}\n",
			want: "option go_package = \"example.com/gen/domain/v1;domainv1\";\n" +
				"/*\n" +
				"message Ghost {\n" +
				"}\n" +
				"*/\n" +
				"message domainv1.Real {\n" +
				"}\n",
		},
		"a block comment opened after a real message does not unmask it": {
			in: "option go_package = \"p;apiv1\";\n" +
				"message Real { /* note\n" +
				"message Ghost {\n" +
				"*/\n",
			want: "option go_package = \"p;apiv1\";\n" +
				"message apiv1.Real { /* note\n" +
				"message Ghost {\n" +
				"*/\n",
		},
		"a real message keeps its trailing line comment verbatim": {
			in: "option go_package = \"p;apiv1\";\n" +
				"message Real { // message Ghost {\n" +
				"}\n",
			want: "option go_package = \"p;apiv1\";\n" +
				"message apiv1.Real { // message Ghost {\n" +
				"}\n",
		},
		"an inline comment before the brace does not hide a real message": {
			in: "option go_package = \"p;apiv1\";\n" +
				"message Real /* renamed from Old */ {\n" +
				"}\n",
			want: "option go_package = \"p;apiv1\";\n" +
				"message apiv1.Real /* renamed from Old */ {\n" +
				"}\n",
		},
		"crlf survives a block comment holding a message": {
			in: "option go_package = \"p;apiv1\";\r\n" +
				"/*\r\n" +
				"message Ghost {\r\n" +
				"*/\r\n" +
				"message Real {\r\n" +
				"}\r\n",
			want: "option go_package = \"p;apiv1\";\r\n" +
				"/*\r\n" +
				"message Ghost {\r\n" +
				"*/\r\n" +
				"message apiv1.Real {\r\n" +
				"}\r\n",
		},
		"an unterminated block comment hides every message after it": {
			in: "option go_package = \"p;apiv1\";\n" +
				"/* scrapped\n" +
				"message Ghost {\n" +
				"}\n",
			want: "option go_package = \"p;apiv1\";\n" +
				"/* scrapped\n" +
				"message Ghost {\n" +
				"}\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(QualifyProtoGoAlias([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatalf("line count changed: %d -> %d", strings.Count(c.in, "\n"), strings.Count(got, "\n"))
			}
		})
	}
}
