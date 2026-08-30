package preprocess

func init() { Register("decomment-go", DecommentGo) }

// DecommentGo blanks // and /* */ comments in Go source, preserving line
// structure. Comment markers inside string and rune literals are untouched.
func DecommentGo(src []byte) []byte {
	out := append([]byte(nil), src...)
	for _, r := range scanGo(src) {
		if r.kind == kindLineComment || r.kind == kindBlockComment {
			blank(out, r.start, r.end)
		}
	}
	return out
}
