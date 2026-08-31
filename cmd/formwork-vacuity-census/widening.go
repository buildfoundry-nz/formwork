package main

// widening.go — the base side of the #15837 arm.
//
// newrules.go answers "which rules does this change ADD?". That question has a
// recall hole, found independently by two streams and reproduced in
// widening_test.go: a rule that already exists and is EDITED into the
// undecidable class keeps its id on BOTH sides of the diff, so it is not an
// addition and walks through on every future PR. A rule can enter vacuity
// without ever being born, and widening a glob is an ordinary refactor edit.
//
// This file supplies the other half. The judgement is a TRANSITION in the
// census's own verdict: undecidable at head, and either absent or decidable at
// the diff's base.
//
// # Why the verdict and not a fingerprint
//
// The obvious alternative is to fingerprint each rule over the fields the
// verdict depends on and re-judge whenever the fingerprint changes. It has
// collateral this does not. A fingerprint fires on ANY edit to a field it
// covers, so rewording a standing offender's cure — or touching its scope for an
// unrelated reason — reds the PR on a defect the author did not write and often
// cannot cure. With 57 standing offenders that is 57 ways to red an unrelated
// change, which is the shape that gets a gate switched off within a week.
//
// Reading the verdict itself has no such collateral, because it asks the
// question the gate actually cares about. A rule that was already undecidable is
// still undecidable, so the transition does not fire and the author is left
// alone. It also cannot drift from the rest of the arm: base and head are
// measured through the SAME two predicates report() counts through.
//
// # Absent is not decidable
//
// A rule missing at base is ABSENT from the returned map, never present with an
// empty reason. Conflating them would make every added rule look as though it
// had been decidable before, which is precisely the silent pass this arm exists
// to close — the caller reads "no entry" as "new", and an empty string as
// "was fine, now is not". Both are refused; they differ only in what the message
// can honestly say.

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

// baseUndecidedReasons returns each rule's undecided reason as of the diff's
// BASE, keyed by rule id. A rule absent at base is ABSENT from the map, never
// present with an empty reason: "this rule did not exist" and "this rule was
// decidable" are different claims, and conflating them makes every added rule
// look as though it had been decidable before.
func baseUndecidedReasons(root, rangeExpr string) (map[string]birthFinding, error) {
	rev, err := diffBaseRev(root, rangeExpr)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "census-base")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	// A base tree with no .formwork/ is the commit that INTRODUCES the corpus.
	// Every rule is then new and none may read as previously decidable, so an
	// empty map is the correct answer and not an error. git archive is the only
	// reader of the base tree here: it costs one pathspec-restricted export of
	// the rule YAML, never a checkout and never a second scan of the worktree.
	if !revHasPath(root, rev, ".formwork") {
		return map[string]birthFinding{}, nil
	}
	if err := exportPath(root, rev, ".formwork", dir); err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		// A base corpus this build cannot parse is not a licence to pass every
		// rule in the change. Refusing is the only safe reading: an unparseable
		// base means the transition cannot be computed, and a transition that
		// cannot be computed must never resolve to "nothing changed".
		return nil, fmt.Errorf("loading the rule corpus at %s: %w", rev, err)
	}
	meta, err := loadRuleMeta(dir)
	if err != nil {
		return nil, fmt.Errorf("reading rule metadata at %s: %w", rev, err)
	}
	declared, err := existsDeclarations(dir)
	if err != nil {
		return nil, fmt.Errorf("reading exists declarations at %s: %w", rev, err)
	}
	out := make(map[string]birthFinding, len(cfg.Rules))
	for _, r := range cfg.Rules {
		// The SAME function head is judged through, so base and head can never be
		// compared through different definitions of the same question.
		out[r.ID] = birthReason(r, meta[r.ID], declared[r.ID])
	}
	return out, nil
}

// diffBaseRev resolves the commit the change is measured FROM — the merge base
// of the range's two endpoints, matching the three-dot semantics
// mergeBaseDiffRange gives the added-rule diff. Both halves of the arm must read
// the same base or a rule could be new to one and old to the other.
func diffBaseRev(root, rangeExpr string) (string, error) {
	left, right := rangeExpr, "HEAD"
	if i := strings.Index(rangeExpr, "..."); i >= 0 {
		left, right = rangeExpr[:i], rangeExpr[i+3:]
	} else if i := strings.Index(rangeExpr, ".."); i >= 0 {
		left, right = rangeExpr[:i], rangeExpr[i+2:]
	}
	if strings.TrimSpace(right) == "" {
		right = "HEAD"
	}
	out, err := gitCmd("-C", root, "merge-base", left, right).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolving the base of %s: %v\n%s", rangeExpr, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// revHasPath reports whether path exists in rev's tree. Distinguishing "the
// corpus was not there yet" from "git failed" matters: the first is an empty
// map, the second must surface.
func revHasPath(root, rev, path string) bool {
	return gitCmd("-C", root, "cat-file", "-e", rev+":"+path).Run() == nil
}

// policyMembers are the members of the exported tree its readers actually open,
// relative to the exported path: config.Load reads the envelope, the rules and
// the allowlists those rules name; loadRuleMeta and existsDeclarations glob the
// rules alone.
//
// fixtures/ and mutations/ are deliberately NOT here. They are 18,392 of the
// corpus's 20,048 files — 67.2 MB of tar against 5.4 MB for the three members
// below — and nothing in the loading path opens one. scripts/ci/rulereach
// narrows the same subtree to the same three members for the same reason.
var policyMembers = []string{"formwork.yaml", "rules", "allowlists"}

// maxEntryBytes bounds one extracted entry, so an archive claiming an
// implausible size cannot fill the disk before the read fails.
const maxEntryBytes = 1 << 30

// exportPath materialises rev's copy of path under dst, narrowed to the members
// above. git archive writes a FILE and the stdlib reads it back: no pipe, and no
// `tar` binary.
//
// No pipe, because `git archive | tar` deadlocked (#16179). The parent held the
// pipe's read end for the whole of archive.Run(), so a consumer that exited
// early left git writing into a full buffer with no EPIPE to end it — a reader
// still existed — and git blocked indefinitely. That is a hang rather than a
// failure, which is the worse of the two for a gate run before every push: a
// failure names a cause and stops, a hang reads as slowness and teaches people
// to skip the gate.
//
// No `tar` binary, because this repo closed the GNU/BSD CLI-divergence class
// deliberately when it ported its last shell files to Go and deleted them
// (#15103 / #14888). A general-purpose CLI dependency inside a .go file
// re-enters that class where no shell rule watches for it.
func exportPath(root, rev, path, dst string) error {
	members := presentMembers(root, rev, path)
	if len(members) == 0 {
		return fmt.Errorf("%s at %s holds none of %v", path, rev, policyMembers)
	}
	tarball, err := os.CreateTemp("", "census-policy-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(tarball.Name())
	if err := tarball.Close(); err != nil {
		return err
	}
	args := append([]string{"-C", root, "archive", "--format=tar", "-o", tarball.Name(), rev}, members...)
	if out, err := gitCmd(args...).CombinedOutput(); err != nil {
		return fmt.Errorf("git archive %s -- %s: %v\n%s", rev, path, err, out)
	}
	if err := extractTar(tarball.Name(), dst); err != nil {
		return fmt.Errorf("extracting %s from %s: %w", path, rev, err)
	}
	if _, err := os.Stat(filepath.Join(dst, path)); err != nil {
		return fmt.Errorf("%s is absent from the exported tree at %s: %w", path, rev, err)
	}
	return nil
}

// presentMembers returns the policy members that exist at rev, as pathspecs
// under path. Probing first is not defensive tidiness: git archive treats a
// pathspec matching nothing as FATAL, so a fixed member list would refuse every
// rev predating any one of them — the commit that introduces allowlists among
// them, and every synthetic corpus the census's own tests build.
func presentMembers(root, rev, path string) []string {
	var out []string
	for _, m := range policyMembers {
		// A git pathspec is slash-separated on every platform, so this is
		// deliberately not filepath.Join.
		if member := path + "/" + m; revHasPath(root, rev, member) {
			out = append(out, member)
		}
	}
	return out
}

// extractTar unpacks archive into dst through the stdlib reader.
func extractTar(archive, dst string) error {
	fh, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer fh.Close()
	root, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	tr := tar.NewReader(fh)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(header.Name))
		// The guard belongs on the reader, not on the trust placed in the
		// producer: an entry naming a path outside dst is never written,
		// whoever wrote the archive.
		if !strings.HasPrefix(target, root+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q escapes the destination", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeEntry(target, tr); err != nil {
				return err
			}
		}
	}
}

// writeEntry writes one regular entry under the size bound.
func writeEntry(target string, tr *tar.Reader) error {
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(tr, maxEntryBytes)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
