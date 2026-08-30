package preprocess

func init() { Register("destring-decomment-sh", DestringDecommentSh) }

// DestringDecommentSh is DestringSh plus comment-body blanking: string
// interiors, heredoc bodies, AND the body of a '#' comment (the '#' included)
// are replaced by spaces. Quotes, heredoc delimiter lines, and line structure
// survive, as with every transform here. It is the third projection of the one
// shell lexer (scanSh), which is why it cannot drift from DestringSh about
// where a string ends: it blanks exactly DestringSh's spans, plus the lexer's
// blankable-comment spans.
//
// The two differ only in what a comment gets. DestringSh skips over a comment
// without touching it — enough to stop an apostrophe inside one from opening a
// phantom string, but the comment text remains matchable. That is wrong for
// any rule whose pattern is a shape the scanned files are expected to DESCRIBE
// as well as avoid: the validating port's shell-meta gates
// (check-no-sigpipe-producer-grep, check-no-negated-searcher-quiet) ban a code shape that every gate header
// comment quotes verbatim, so matching against destring-sh output alone reports
// the documentation as a violation. Those gates pipe destring-sh.awk through a
// sed comment-stripper for exactly this reason; this transform is that pair in
// one pass, and inherits the correct heredoc-in-comment handling (issue #6)
// that the awk version lacks.
//
// The blanked-comment boundary is NARROWER than the boundary DestringSh uses to
// merely SKIP a comment, and the two must not be conflated. A '#' is only
// blanked when it begins a line or follows whitespace — the reference stripper's
// `sed -E 's/(^|[[:space:]])#.*$//'` rule. It is *recognized* as a comment (and
// skipped) after the wider word-breaking set ';', '|', '&', '(', ')', '<', '>'
// too, but blanking there would destroy live code — `x=$(ls)#tag` is a single
// shell word — so a comment introduced by one of those characters is left
// intact, exactly as the sed leaves it. That distinction is enforced inside the
// lexer, which reports the wide set as skipped and only the narrow set as
// blankable (isCommentBlankBoundary). A rule reading this transform therefore
// must NOT assume every comment is blanked: a `cmd;# ...` comment survives into
// the scanned view. In both cases ${x#foo}, $#, and url#fragment are ordinary
// text, never comment starts.
func DestringDecommentSh(src []byte) []byte {
	out := append([]byte(nil), src...)
	data, comments := scanSh(src)
	for _, s := range data {
		blank(out, s.start, s.end)
	}
	for _, s := range comments {
		blank(out, s.start, s.end)
	}
	return out
}
