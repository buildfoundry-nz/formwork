// Package declspan answers one question: which lines does the declaration
// that a given line opens span?
//
// It exists so an excusal can be as narrow as the thing it names. A
// credential (forbidden-pattern require_absent / none_of) and a
// scope.not_declaring marker both name a HOME — "this declaration is the one
// place that may do X". Before 0.14 the engine took the whole FILE holding the
// home as excused, so a second violation added anywhere in that file, outside
// the home, was invisible to the rule. The home is now the declaration the
// marker's line opens, and every other line of the file is judged.
//
// What a declaration is depends on the language:
//
//   - Go: the top-level statement the line's first token begins, read with
//     go/scanner so strings, raw strings, runes and comments cannot fake a
//     delimiter. It ends at the first statement terminator (an explicit or
//     automatically inserted semicolon) at bracket depth zero: a func or
//     method ends after its body's closing brace, a const/var/type spec after
//     its value or type. A `package` clause is the one Go declaration whose
//     extent is the whole file: the package is the unit Go organizes code by.
//   - Dart and proto: the declaration from the line's first character to the
//     closing brace of its first depth-zero block, or a semicolon at depth
//     zero, whichever comes first, scanned over a projection whose comment and
//     string bytes are blank (preprocess.CodeOnlyDart for Dart, a C-style
//     decomment for proto) so neither can fake a delimiter.
//   - Anything else (Markdown, YAML, SQL, plain text) has no declaration
//     structure this package bounds, so a document's marker declares the
//     document: the extent is the whole file.
package declspan

import (
	"bytes"
	"go/scanner"
	"go/token"
	"path"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
)

// Span is a 1-based, inclusive line range.
type Span struct{ Start, End int }

// Contains reports whether line lies in the span.
func (s Span) Contains(line int) bool { return line >= s.Start && line <= s.End }

// Extent returns the lines spanned by the declaration that line (1-based)
// opens in the file at path with content src. A line past the end of the file
// yields that line alone.
func Extent(path string, src []byte, line int) Span {
	last := lineCount(src)
	if line < 1 || line > last {
		return Span{line, line}
	}
	switch lang(path) {
	case "go":
		return goExtent(src, line, last)
	case "dart":
		return braceExtent(preprocess.CodeOnlyDart(src), line, last)
	case "proto":
		return braceExtent(decommentC(src), line, last)
	default:
		return Span{1, last}
	}
}

// Directives returns the file-scoped directives of the file at path: Go
// import declarations, and Dart import/export/part/library directives. An
// import serves every declaration in its file, so no declaration can enclose
// it; a file that holds a home owns its directives with it. Other languages
// have none this package bounds.
func Directives(path string, src []byte) []Span {
	switch lang(path) {
	case "go":
		return goDirectives(src)
	case "dart":
		return dartDirectives(preprocess.CodeOnlyDart(src))
	}
	return nil
}

func goDirectives(src []byte) []Span {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, 0)
	var out []Span
	depth, inImport, start := 0, false, 0
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			return out
		}
		switch tok {
		case token.IMPORT:
			if depth == 0 && !inImport {
				inImport, start = true, file.Line(pos)
			}
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if depth > 0 {
				depth--
			}
		case token.FUNC, token.TYPE, token.VAR, token.CONST:
			if depth == 0 {
				// Imports precede every other declaration in a Go file.
				return out
			}
		case token.SEMICOLON:
			if depth == 0 && inImport {
				out = append(out, Span{start, file.Line(pos)})
				inImport = false
			}
		}
	}
}

// dartDirectives scans code (comments and string contents blanked) for
// top-level directives: a line whose first word at depth zero is import,
// export, part or library, through its terminating semicolon.
func dartDirectives(code []byte) []Span {
	var out []Span
	depth, line, inDir, start := 0, 1, false, 0
	atLineStart := true
	for i := 0; i < len(code); i++ {
		c := code[i]
		if c == '\n' {
			line++
			atLineStart = true
			continue
		}
		if atLineStart && c != ' ' && c != '\t' {
			atLineStart = false
			if depth == 0 && !inDir {
				for _, kw := range []string{"import ", "export ", "part ", "library "} {
					if bytes.HasPrefix(code[i:], []byte(kw)) {
						inDir, start = true, line
						break
					}
				}
			}
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 && inDir {
				out = append(out, Span{start, line})
				inDir = false
			}
		}
	}
	return out
}

// Whole reports whether a span covers every line of src.
func Whole(src []byte, s Span) bool { return s.Start <= 1 && s.End >= lineCount(src) }

// Mask returns a copy of src with every byte on a line inside any span
// replaced by a space, newlines kept, so line numbers and byte offsets stay
// the file's own and a masked declaration contributes nothing to a match.
func Mask(src []byte, spans []Span) []byte {
	if len(spans) == 0 {
		return src
	}
	out := append([]byte(nil), src...)
	line := 1
	for i, b := range out {
		if b == '\n' {
			line++
			continue
		}
		for _, s := range spans {
			if s.Contains(line) {
				out[i] = ' '
				break
			}
		}
	}
	return out
}

func lang(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return "go"
	case ".dart":
		return "dart"
	case ".proto":
		return "proto"
	}
	return ""
}

// lineCount is the number of lines Lines() would return, counting a final
// unterminated line.
func lineCount(src []byte) int {
	if len(src) == 0 {
		return 0
	}
	n := bytes.Count(src, []byte("\n"))
	if src[len(src)-1] != '\n' {
		n++
	}
	return n
}

func goExtent(src []byte, line, last int) Span {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, 0)
	depth, started := 0, false
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			return Span{line, last}
		}
		at := file.Line(pos)
		if !started {
			if at < line {
				continue
			}
			if at > line {
				// Nothing on the line opens a statement (a blank or comment
				// line): the line alone is all it can be said to declare.
				return Span{line, line}
			}
			started = true
			if tok == token.PACKAGE {
				return Span{1, last}
			}
		}
		switch tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if depth > 0 {
				depth--
			}
		case token.SEMICOLON:
			if depth == 0 {
				return Span{line, at}
			}
		}
	}
}

// braceExtent scans code (a projection with comments and string contents
// blanked) from the start of line.
func braceExtent(code []byte, line, last int) Span {
	off, cur := 0, 1
	for cur < line && off < len(code) {
		if code[off] == '\n' {
			cur++
		}
		off++
	}
	depth := 0
	for i := off; i < len(code); i++ {
		switch code[i] {
		case '\n':
			cur++
		case '(', '[', '{':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case '}':
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				return Span{line, cur}
			}
		case ';':
			if depth == 0 {
				return Span{line, cur}
			}
		}
	}
	return Span{line, last}
}

// decommentC blanks // and /* */ comments and the contents of "..." and '...'
// strings, keeping newlines, which is all proto needs.
func decommentC(src []byte) []byte {
	out := append([]byte(nil), src...)
	n := len(src)
	for i := 0; i < n; i++ {
		switch {
		case i+1 < n && src[i] == '/' && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				out[i] = ' '
				i++
			}
		case i+1 < n && src[i] == '/' && src[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i < n && !(i+1 < n && src[i] == '*' && src[i+1] == '/') {
				if src[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if i+1 < n {
				out[i], out[i+1] = ' ', ' '
				i++
			}
		case src[i] == '"' || src[i] == '\'':
			q := src[i]
			i++
			for i < n && src[i] != q && src[i] != '\n' {
				if src[i] == '\\' && i+1 < n {
					out[i] = ' '
					i++
				}
				out[i] = ' '
				i++
			}
		}
	}
	return out
}
