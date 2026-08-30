package preprocess

func init() { Register("strings-only-go", StringsOnlyGo) }

// StringsOnlyGo keeps only the interiors of Go string literals (interpreted
// and raw); everything else — code, comments, rune literals, and the quotes
// themselves — is blanked. Used by rules that match content carried in
// strings (e.g. SQL).
func StringsOnlyGo(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank(out, 0, len(out))
	for _, r := range scanGo(src) {
		if r.kind != kindString {
			continue
		}
		start, end := r.start+1, r.end
		// Drop the closing quote if the literal was terminated.
		if end > start && (src[end-1] == '"' || src[end-1] == '`') {
			end--
		}
		copy(out[start:end], src[start:end])
	}
	return out
}
