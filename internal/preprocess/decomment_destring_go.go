package preprocess

func init() { Register("decomment-destring-go", DecommentDestringGo) }

// DecommentDestringGo blanks Go comments AND the interiors of string and rune
// literals, preserving line structure. Only real code survives.
//
// decomment-go alone is not enough for a required-pattern that must prove a
// call is still made: the symbol surviving in a string literal satisfies the
// matcher just as a comment used to. For the entitlements lockdowns that is the
// natural shape of a refactor rather than a contrivance — the package already
// carries several fmt.Errorf("entitlements.ConsumeTakeoff: %w", err) strings,
// so the call can be deleted while the error-wrap string keeps the gate green.
//
// The quotes themselves are kept so the result stays recognisably Go and a
// pattern anchored on a delimiter still behaves; only the contents go.
func DecommentDestringGo(src []byte) []byte {
	out := append([]byte(nil), src...)
	for _, r := range scanGo(src) {
		switch r.kind {
		case kindLineComment, kindBlockComment:
			blank(out, r.start, r.end)
		case kindString, kindRune:
			// Interior only: scanGo's span includes the delimiters, so step in
			// one at each end. The closing quote is dropped from the blanked
			// range only when the literal was actually terminated — an
			// unterminated literal at EOF has no closing delimiter to keep,
			// and blanking past its end would be a slice overrun.
			start, end := r.start+1, r.end
			if end > start {
				switch src[end-1] {
				case '"', '`', '\'':
					end--
				}
			}
			if end > start {
				blank(out, start, end)
			}
		}
	}
	return out
}
