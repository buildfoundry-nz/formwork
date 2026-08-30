package preprocess

// kind classifies a region of Go source.
type kind int

const (
	kindCode         kind = iota
	kindLineComment       // // to end of line (newline excluded)
	kindBlockComment      // /* ... */ delimiters included
	kindString            // interpreted "…" or raw `…`, quotes included
	kindRune              // '…' quotes included
)

// region is a half-open byte range [start, end) of one kind.
type region struct {
	kind       kind
	start, end int
}

// scanOpts states the dialect scanCLike is reading. The C-like languages this
// package serves share `//`, `/* */` and quoted literals; they disagree about
// the backtick, and a scanner that guessed from the bytes would guess wrong in
// exactly the file where it matters. Each caller says which language it holds.
type scanOpts struct {
	// rawStrings makes a backtick open a raw string literal running to the
	// next backtick, or to EOF if there is none — Go's rule. A language with
	// no raw-string form sets it false, so a stray backtick is ordinary code
	// rather than an opening delimiter that swallows the rest of the file and
	// hides every comment after it from a caller masking comments out.
	rawStrings bool
}

// scanGo partitions src into regions by Go literal/comment structure; code
// regions carry everything else. Unterminated literals and comments run to
// end of line (interpreted strings, runes) or EOF (raw strings, block
// comments), matching how far their content can extend.
func scanGo(src []byte) []region { return scanCLike(src, scanOpts{rawStrings: true}) }

// scanCLike is the shared body of scanGo and its siblings: the same partition,
// with the dialect differences in opts.
func scanCLike(src []byte, opts scanOpts) []region {
	var regs []region
	i, n, mark := 0, len(src), 0
	flushCode := func(upto int) {
		if upto > mark {
			regs = append(regs, region{kindCode, mark, upto})
		}
	}
	emit := func(k kind, end int) {
		regs = append(regs, region{k, i, end})
		mark, i = end, end
	}
	for i < n {
		switch c := src[i]; {
		case c == '/' && i+1 < n && src[i+1] == '/':
			flushCode(i)
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			emit(kindLineComment, j)
		case c == '/' && i+1 < n && src[i+1] == '*':
			flushCode(i)
			j := i + 2
			for j+1 < n && !(src[j] == '*' && src[j+1] == '/') {
				j++
			}
			if j+1 < n {
				j += 2
			} else {
				j = n
			}
			emit(kindBlockComment, j)
		case c == '"' || c == '\'':
			flushCode(i)
			j := i + 1
			for j < n && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' && j+1 < n && src[j+1] != '\n' {
					j++
				}
				j++
			}
			if j < n && src[j] == c {
				j++
			}
			k := kindString
			if c == '\'' {
				k = kindRune
			}
			emit(k, j)
		case c == '`' && opts.rawStrings:
			flushCode(i)
			j := i + 1
			for j < n && src[j] != '`' {
				j++
			}
			if j < n {
				j++
			}
			emit(kindString, j)
		default:
			i++
		}
	}
	flushCode(n)
	return regs
}

// blank overwrites dst[start:end) with spaces, preserving newlines.
func blank(dst []byte, start, end int) {
	for i := start; i < end; i++ {
		if dst[i] != '\n' {
			dst[i] = ' '
		}
	}
}
