package preprocess

func init() { Register("strings-only-sh", StringsOnlySh) }

// StringsOnlySh keeps ONLY the interiors of shell quoted strings and heredoc
// bodies; shell text — code, comments, and the quotes and delimiter lines
// themselves — is blanked. It is the exact complement of DestringSh over the
// same lexer (scanShData), so what one drops the other keeps, byte for byte.
// That is the reason the span extractor exists: the pair cannot be written as
// two independent scanners without eventually disagreeing about where a string
// ends, and a disagreement is a byte no rule reads at all.
//
// It exists to read EMBEDDED PROGRAM TEXT: the awk programs shell scripts
// carry as single-quoted arguments or heredoc bodies, and the `bash -c '…'`
// worker bodies. That text is a program with its own '#' comments, but to the
// shell lexer it is opaque data, so DestringSh blanks it and every rule
// reading that projection is blind to it. Without this projection a ban on a
// token stops at the quote — the same token moved inside an awk program is
// unreachable by the rule that forbids it.
//
// The two projections are deliberately separate rather than one union: a
// consumer must not confuse a '#' that opens a SHELL comment with a '#' that
// opens a comment in some other language that happens to be stored in a shell
// string. Rules that read this one are stating that they mean the latter.
func StringsOnlySh(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank(out, 0, len(out))
	for _, s := range scanShData(src) {
		copy(out[s.start:s.end], src[s.start:s.end])
	}
	return out
}
