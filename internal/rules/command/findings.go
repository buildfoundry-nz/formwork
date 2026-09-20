package command

import "github.com/buildfoundry-nz/formwork/internal/rules"

// findingsFormat is the one declared output contract for a command detector,
// named in params.output. A rule that declares nothing keeps today's single
// pathless message, so an existing corpus loads and behaves exactly as before.
const findingsFormat = "findings-v1"

// parseFindings turns a conforming detector's output into structured findings.
//
// The convention is the one every Go tool already speaks — `path: message` or
// `path:line: message` — so a detector adopting it usually only has to print
// what it already knows in a different order.
//
// WHY THE ENGINE AND NOT THE LOADER. The alternative was regexes per detector
// in whatever consumes the output, which is N patterns drifting from N tools:
// the divergence shape this estate refuses on sight. Parsing here means a
// detector either conforms and yields locations or declares that it cannot,
// and the count of rules that cannot is a disclosed number rather than a
// silence.
//
// exists reports whether a repo-relative path is in the tree under
// evaluation. It is the DISCRIMINATOR, not punctuation: plenty of ordinary
// output reads as "word: prose", and minting a finding against a path that is
// not there would send a reader somewhere real and unrelated — strictly worse
// than the pathless finding this replaces.
//
// Nothing is dropped. Lines that are not locations are joined into a single
// pathless finding, because a contract that silently loses a detector's cure
// trades one blindness for another.
func parseFindings(out []byte, exists func(rel string) bool) []rules.Match {
	panic("not implemented")
}
