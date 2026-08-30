package preprocess

import (
	"bytes"
	"regexp"
)

func init() { Register("qualify-proto-go-alias", QualifyProtoGoAlias) }

// goPackageAliasRe pulls the quoted go_package option value. Proto files in
// the consuming repos use the standard `option go_package = "path;alias";`
// spelling; a missing or unquoted option is treated as no alias. It is run
// over a comment-masked copy (maskProtoComments), never over raw source: the
// pattern cannot tell a declaration from the same line inside a /* */ block.
var goPackageAliasRe = regexp.MustCompile(`(?m)^\s*option\s+go_package\s*=\s*"([^"]*)"`)

// topLevelMessageRe matches a column-0 proto message whose opening brace is
// on the same line — the shape set-relation A-side patterns already require.
// Nested messages are indented and must stay bare so they are not extracted.
// Like goPackageAliasRe it is run over the comment mask, never over the raw
// line: at column 0 the pattern cannot tell a declaration from the same words
// sitting inside a /* */ block, and a message it qualifies from inside one is
// a name no consuming rule can find in the generated Go.
var topLevelMessageRe = regexp.MustCompile(`^message\s+([A-Za-z][A-Za-z0-9_]*)(\s*)\{`)

// QualifyProtoGoAlias prefixes each top-level `message Name {` with the file's
// go_package alias (`message domainv1.Name {`). It is line-preserving: same
// newline count as the input, CRLF kept as CRLF. Files with no `;alias` in
// go_package are returned unchanged so a missing alias is fail-closed at the
// rule (A emits a bare name that B will not contain) rather than invented.
//
// Both scans read the same comment mask and every emitted byte comes from the
// original. Only a message the file actually declares is qualified, and the
// one it declares is reproduced exactly — its own inline and trailing comments
// included, which a rewrite that emitted the masked bytes would blank out.
func QualifyProtoGoAlias(src []byte) []byte {
	out := append([]byte(nil), src...)
	masked := maskProtoComments(out)
	alias := goPackageAlias(masked)
	if alias == "" {
		return out
	}
	prefix := []byte("message " + alias + ".")
	var buf []byte
	at := 0
	for _, line := range splitKeepNL(out) {
		body, nl := cutNL(line)
		// masked is length-preserving, so this slice is the same byte
		// range as body and an offset into it indexes body directly.
		loc := topLevelMessageRe.FindSubmatchIndex(masked[at : at+len(body)])
		at += len(line)
		if loc == nil {
			buf = append(buf, line...)
			continue
		}
		// loc[2]:loc[3] is the message name; keep whatever followed it
		// (whitespace + `{` + rest of line, including a trailing comment).
		buf = append(buf, prefix...)
		buf = append(buf, body[loc[2]:]...)
		buf = append(buf, nl...)
	}
	return buf
}

// maskProtoComments returns a copy of src with every comment byte replaced by
// a space. It is length-preserving and never touches a newline, so an offset
// into the mask indexes the same byte of the original and the two split into
// the same lines — which is what lets a pattern be matched against the mask
// while every byte emitted comes from the original.
//
// String contents survive. A go_package path is URL-shaped and may carry `//`
// inside its quotes; the scanner takes the opening quote before it can reach
// those bytes, so they are a string region and stay readable to the alias
// scan. Masking string contents as well would erase the alias this transform
// exists to read.
//
// The dialect is proto: scanOpts.rawStrings stays false because proto has no
// raw-string literal. Under Go's rule a backtick that reached code position —
// a line pasted back in with its markdown quoting, say — would open a string
// running to EOF, every /* */ after it would stop being a comment, and the
// mask would quietly stop masking. That is the direction that lets a dead
// option speak for the file, so the scanner is told which language it holds.
func maskProtoComments(src []byte) []byte {
	masked := append([]byte(nil), src...)
	for _, r := range scanCLike(src, scanOpts{}) {
		if r.kind == kindLineComment || r.kind == kindBlockComment {
			blank(masked, r.start, r.end)
		}
	}
	return masked
}

// goPackageAlias returns the alias of the FIRST go_package option in src, and
// "" if that option carries none. src must be comment-masked — the pattern
// reads a commented-out option as a live one otherwise.
//
// FIRST, not last. A proto declares go_package once, so a second one is a
// duplicate or a leftover, and a live option must not be overridable by
// anything written below it. The same reading governs an option that yields
// no usable alias: a first option with no `;alias`, or one whose alias is not
// a Go identifier, means the file has no alias. Reading further down to rescue
// it is how a stale option gets to speak for the file — and no alias is the
// fail-closed answer, since the transform then leaves messages bare and the
// consuming rule reports the missing declaration.
func goPackageAlias(src []byte) string {
	m := goPackageAliasRe.FindSubmatch(src)
	if m == nil {
		return ""
	}
	val := string(m[1])
	i := lastByte(val, ';')
	if i < 0 || i+1 >= len(val) {
		return ""
	}
	if cand := val[i+1:]; isGoIdent(cand) {
		return cand
	}
	return ""
}

func lastByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func isGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		ok := b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (i > 0 && b >= '0' && b <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// splitKeepNL splits src into lines that each include their trailing newline
// (or `\r\n`). A final line without a newline is returned as-is so the
// rewrite cannot invent one.
func splitKeepNL(src []byte) [][]byte {
	if len(src) == 0 {
		return nil
	}
	var lines [][]byte
	start := 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lines = append(lines, src[start:i+1])
			start = i + 1
		}
	}
	if start < len(src) {
		lines = append(lines, src[start:])
	}
	return lines
}

func cutNL(line []byte) (body, nl []byte) {
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return line[:len(line)-2], line[len(line)-2:]
	}
	if bytes.HasSuffix(line, []byte("\n")) {
		return line[:len(line)-1], line[len(line)-1:]
	}
	return line, nil
}
