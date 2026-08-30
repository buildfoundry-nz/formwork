package cli

// rules-for (#108, spec §10): the pre-hoc guidance query, split from
// introspect.go when the 750-line vendor cap fired. The NOT SCANNED
// dispatch, the query normalizer, and walk-reachability classification all
// live here; explain/list and the shared format/emit helpers stay in
// introspect.go.

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// govRule is one governing rule in rules-for output — the fields a pre-hoc
// guidance consumer needs (#108): what applies, how bad a
// violation is, and the written cure/origin.
type govRule struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Cost       string `json:"cost"`
	Preprocess string `json:"preprocess,omitempty"`
	Cure       string `json:"cure,omitempty"`
	Origin     string `json:"origin,omitempty"`
	// SuppressedBy carries a path-exact allowlist exemption in the engine's
	// own finding.SuppressedBy shape ("allowlist:<file>:<line>"): the path
	// is governed, but every finding on it is suppressed.
	SuppressedBy string `json:"suppressed_by,omitempty"`
}

// notScannedOut says why the walk never visits this path: the answer to a
// governance query about an unscanned path is "nothing enforces here — and
// here is the declared reason", never a rule list asserting enforcement that
// check will not perform (fail-open review finding 2 — scan.ignore is the
// widest exemption channel in the engine, and it must stay visible here).
type notScannedOut struct {
	By   string `json:"by"` // "scan.ignore", "scan.gitignore", "builtin-skip", or "non-regular"
	Glob string `json:"glob,omitempty"`
	// Rule is the deciding gitignore line in the census's own
	// "<file>:<line>:<pattern>" shape, set only when By is "scan.gitignore".
	Rule   string `json:"rule,omitempty"`
	Reason string `json:"reason,omitempty"`
	// ExternalToolRules names the command/git-diff rules whose external
	// tools re-scan the tree on their own and may still reach this path —
	// "not scanned" is a statement about the walk, not about them.
	ExternalToolRules []string `json:"external_tool_rules,omitempty"`
}

// pathRules groups governing rules under one queried path.
type pathRules struct {
	Path       string         `json:"path"`
	Rules      []govRule      `json:"rules"`
	NotScanned *notScannedOut `json:"not_scanned,omitempty"`
}

func runRulesFor(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("rules-for", "repository root (default \".\")", stderr)
	format := fs.String("format", "human", "output format: human | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !introspectFormat(*format, stderr) {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "usage: formwork rules-for [flags] <path>... (root-relative; the path need not exist — scope is a glob question)")
		return 2
	}
	// A corpus with NO rules cannot answer "what governs this path" — and the
	// honest answer is not "(none)". This command is the pre-hoc guidance
	// primitive: an external consumer asks it before an edit and is told what
	// constrains the file. "(none)" for a path no rule matches is correct and
	// must stay; "(none)" because the engine loaded nothing is a wrong frame,
	// and answering it confidently tells a caller pointed at a bad checkout, the
	// wrong -C, or a .formwork/rules that never materialised that their file is
	// unconstrained. That is the guidance fail-open this command already refuses
	// for out-of-root paths, arriving through a different door.
	//
	// Placed here rather than left to check/lint/test's own guards: those three
	// refusing while this one answered is what made the family inconsistent —
	// the same config, and one command still returning a confident empty answer
	// to a consumer that acts on it.
	//
	// `list rules` and `list lanes` are covered too, as of #157, and the refusal
	// is now literally one function (loadGatedNonEmpty in introspect.go) rather
	// than this guard plus a copy. That question was open when this comment
	// first argued rules-for's case: an earlier version of it called rules-for
	// "the only caller that is a machine", which is false — `-format json` makes
	// list a machine surface too, and #155 excluded list on exactly the reasoning
	// #157 overturned. What settled it is that the family was INCONSISTENT: a
	// consumer that learns "formwork refuses a config it could not load" here
	// carries that assumption to list, which answered `[]`.
	//
	// The registry kinds (`list types`, `list preprocessors`) stay outside it —
	// they never load config at all.
	cfg, ok := loadGatedNonEmpty(*root, "no rule can govern any path and an empty answer here would read as \"unconstrained\"", stderr)
	if !ok {
		return 2
	}
	// External-tool rules (command/git-diff — the CostHeavy class) exec
	// whole-tree tools that re-scan on their own: a NOT SCANNED answer that
	// omitted them would deny enforcement check actually performs (#119
	// review finding 2).
	externalTools := []string{}
	for _, r := range cfg.Rules {
		if r.Cost() == rules.CostHeavy {
			externalTools = append(externalTools, r.ID)
		}
	}
	// The walk's third hiding channel (#100): resolved through the same seam
	// check and lint use, never re-derived. check may degrade softly when git
	// cannot answer — pruning nothing only WIDENS its scan — but a governance
	// ANSWER that silently dropped a declared prune channel could assert
	// scanning the enforcing environment never performs, so here unanswerable
	// is loud (#122; vcs package contract: an error is never read as "nothing
	// is ignored"). An undeclared key never touches git at all.
	// An unanswerable snapshot is HELD, not returned: the same precedence
	// carve-out the ghost-batch failure gets (spec §10) — a verdict
	// decidable without git still answers, and only a query whose verdict
	// depends on the channel refuses (#125 round-3 finding 2; a blanket
	// refusal dropped glob/builtin answers main gave without touching git).
	gitIgnore := meta.ResolveGitIgnore(cfg, *root)
	norm := newQueryNormalizer(*root)
	// Pass 1: normalize and classify every argument, collecting the ghost
	// candidates the whole invocation needs — one check-ignore batch serves
	// all queries (#125 round-2 finding 3), and existence is a property of
	// the filesystem at this moment, so a prefix's verdict is equally valid
	// for every query that shares it.
	globs := cfg.IgnoreGlobs()
	type query struct {
		p          string
		nonRegular bool
		ghostFrom  int  // index of first missing segment, -1 when all exist
		needsGit   bool // its verdict may depend on the check-ignore batch
		wrErr      error
	}
	queries := make([]query, 0, fs.NArg())
	var candidates []string
	seenCandidate := map[string]bool{}
	var submodules []string
	submodulesLoaded := false
	var ghostErr error
	for _, arg := range fs.Args() { // argv order preserved
		p, err := norm.normalize(arg)
		if err != nil {
			// A wrong-frame path must be LOUD: an empty result here would
			// read as "no rules govern this file" — a guidance fail-open.
			fmt.Fprintf(stderr, "formwork: %v\n", err)
			return 2
		}
		// Classify the on-disk state now — the ghost index decides what the
		// batch must ask — but surface any error only after the hidden
		// check below, preserving the walk's own precedence (prunes before
		// the symlink refusal, so a hidden-but-unclassifiable path still
		// answers hidden).
		nonRegular, ghostFrom, wrErr := walkReachability(*root, p)
		q := query{p: p, nonRegular: nonRegular, ghostFrom: ghostFrom, wrErr: wrErr}
		// Even a glob-hidden ghost still contributes candidates: a gitignore
		// prune at a SHALLOWER level than the glob's match outranks it in
		// the walk's order, and knowing that requires git's answer. That
		// cannot poison a sibling — pass 2's hidden check runs before the
		// ghostErr refusal, so a query answerable without git answers even
		// when the batch fails; only the submodule shape (below) is a
		// per-candidate fatal and is excluded before the batch.
		if wrErr == nil && ghostFrom >= 0 && gitIgnore.State == meta.GitIgnoreOn {
			if !submodulesLoaded {
				// check-ignore FATALS (exit 128) on any pathspec under a
				// registered submodule — while the walk is
				// submodule-OBLIVIOUS: it scans whatever plain files sit
				// there and the snapshot never prunes them (verified: check
				// enforces files written into submodule paths). So for this
				// engine those candidates are simply not git-ignored, and
				// they are excluded BEFORE the batch rather than surfaced
				// as an unanswerable channel. A failure listing them is
				// held like a batch failure.
				submodulesLoaded = true
				if submodules, err = vcs.Submodules(*root); err != nil {
					ghostErr = err
				}
			}
			for _, c := range ghostCandidates(p, ghostFrom) {
				if underAny(strings.TrimSuffix(c, "/"), submodules) {
					continue
				}
				q.needsGit = true
				if !seenCandidate[c] {
					seenCandidate[c] = true
					candidates = append(candidates, c)
				}
			}
		}
		queries = append(queries, q)
	}
	// The ghost re-ask: the resolved snapshot covers only paths that EXIST,
	// and guidance is asked about files not yet written, so git is asked
	// directly — one batch for the whole invocation. check-ignore evaluates
	// patterns for any pathname; omitting --no-index keeps git's tracked
	// carve-out; negation-decided records are filtered at the vcs seam. A
	// batch failure is HELD, not returned: a verdict decidable without git
	// still answers (the same precedence deferral wrErr gets), and only a
	// query that actually needs the ghost answer refuses.
	//
	// FRAME AND GATING are load-bearing (#125 round-3 + Opus parity
	// findings): the LEAF (file-frame) verdict is the only sound
	// hidden/not-hidden decision for a ghost — git resolves the full path
	// through every ancestor dir pattern AND every negation, exactly as it
	// will the moment the file lands. A DIR-frame candidate answers "does a
	// pattern match this string", which is NOT "would the walk prune this
	// directory": `gen/*` matches the string "gen/" while `!gen/keep/`
	// re-includes the queried descendant. So dir-frame records never decide
	// hiding; they only refine the prune LEVEL (for cross-channel ordering
	// and attribution) once the leaf verdict says ignored — assembled
	// per-query in pass 2, so no record can leak across queries.
	leafGhost := map[string]string{}
	ghostDirs := map[string]string{}
	if len(candidates) > 0 && ghostErr == nil {
		if recs, err := checkIgnored(*root, candidates); err != nil {
			ghostErr = err
		} else {
			for _, r := range recs {
				if r.Dir {
					ghostDirs[r.Path] = r.Rule()
				} else {
					leafGhost[r.Path] = r.Rule()
				}
			}
		}
	}
	// Pass 2: answer each query. Existing paths judge against the snapshot;
	// a ghost judges against its leaf-gated overlay — nil when git says the
	// landed file would NOT be ignored, so today's snapshot collapse of an
	// ancestor cannot contradict what the walk does the moment the file
	// lands (spec §10's frame; Opus parity secondary observation).
	out := make([]pathRules, 0, len(queries))
	for _, q := range queries {
		p := q.p
		pr := pathRules{Path: p, Rules: []govRule{}}
		view := gitIgnore.Set
		if q.ghostFrom >= 0 {
			view = ghostOverlay(p, q.ghostFrom, leafGhost, ghostDirs, gitIgnore.Set)
		}
		opts := scan.Opts{Ignore: globs, GitIgnored: view}
		// A path the walk never visits must never get a governing-rules
		// answer: that would assert enforcement check will not perform.
		// scan.ignore is the widest exemption channel in the engine and it
		// stays visible here — the annotation names the glob and its
		// declared reason (fail-open review finding 2). Attribution is the
		// walk's own order via scan.NotScannedBy: shallowest trigger wins,
		// builtin before globs before the gitignore prune per level — so a
		// .git nested under an ignored tree names the operator glob, exactly
		// as lint would (#119 review finding 6).
		if v := scan.NotScannedBy(p, opts); v.Hidden() {
			ns, err := notScannedAnswer(v, cfg, externalTools)
			if err != nil {
				fmt.Fprintf(stderr, "formwork: %v\n", err)
				return 2
			}
			pr.NotScanned = ns
			out = append(out, pr)
			continue
		}
		// Not hidden by anything decidable so far. If the declared channel's
		// snapshot could not be resolved at all, EVERY remaining answer for
		// this query could depend on it — a governed list (the path might be
		// snapshot-pruned) as much as a non-regular attribution — so the
		// refusal fires here, after the git-free channels had their say.
		if gitIgnore.State == meta.GitIgnoreUnknown {
			return gitignoreUnanswerable(stderr, gitIgnore.Err)
		}
		// If this query's own candidates entered the failed batch, the
		// question is unanswerable and the refusal fires now, never a
		// confident governed answer. A query whose candidates were all
		// excluded (git-free-hidden, or under a submodule the walk is
		// oblivious to) never needed git and answers normally.
		if q.needsGit && ghostErr != nil {
			return gitignoreUnanswerable(stderr, ghostErr)
		}
		// A path that cannot be classified (stat error, regular-file
		// ancestor) is loud, never a confident answer (#119 third-pass
		// findings 1b/3).
		if q.wrErr != nil {
			fmt.Fprintf(stderr, "formwork: %v\n", q.wrErr)
			return 2
		}
		// The walk's last skip channel: a non-regular entry (symlink, fifo)
		// at the leaf OR any ancestor is never yielded/descended — listing
		// rules for such a path would assert enforcement that never happens
		// (#119 review finding 5; ancestors, third-pass finding 1).
		if q.nonRegular {
			pr.NotScanned = &notScannedOut{By: "non-regular", ExternalToolRules: externalTools}
			out = append(out, pr)
			continue
		}
		for _, r := range cfg.Rules { // Load sorts by id
			if !r.Applies(p) {
				continue
			}
			g := govRule{
				ID: r.ID, Type: r.Type, Severity: string(r.Severity),
				Cost: string(r.Cost()), Preprocess: r.Preprocess,
				Cure: r.Cure, Origin: r.Origin,
			}
			// Allowlist exemption is path-exact and knowable pre-hoc: the
			// path IS governed, but every finding on it is suppressed —
			// display carries the suppression in the engine's own
			// SuppressedBy shape rather than reporting bare governance
			// (#119 review finding 4; except.paths differs on purpose —
			// there Applies() is false and no finding ever exists).
			if r.Allowlist != nil {
				for _, e := range r.Allowlist.Entries {
					if e.Path == p {
						g.SuppressedBy = fmt.Sprintf("allowlist:%s:%d", r.Allowlist.File, e.Line)
						break
					}
				}
			}
			pr.Rules = append(pr.Rules, g)
		}
		out = append(out, pr)
	}
	if *format == "json" {
		return emitJSON(stdout, stderr, out)
	}
	for _, pr := range out {
		fmt.Fprintf(stdout, "%s:\n", pr.Path)
		if ns := pr.NotScanned; ns != nil {
			switch ns.By {
			case "scan.ignore":
				fmt.Fprintf(stdout, "  NOT SCANNED — scan.ignore (%s): %s\n", ns.Glob, ns.Reason)
			case "scan.gitignore":
				fmt.Fprintf(stdout, "  NOT SCANNED — scan.gitignore (%s): %s\n", ns.Rule, ns.Reason)
			case "non-regular":
				fmt.Fprintln(stdout, "  NOT SCANNED — not a regular file (the walk never scans symlinks or special files; a source-named symlink refuses the whole scan)")
			case "builtin-skip":
				fmt.Fprintln(stdout, "  NOT SCANNED — built-in skip (.git and .formwork are never scanned)")
			default:
				// Unreachable from our own constructors — but a new channel
				// must fail closed here, not render under the builtin-skip
				// wording (#125 round-2; same stance as notScannedAnswer).
				fmt.Fprintf(stderr, "formwork: internal: unknown not-scanned channel %q\n", ns.By)
				return 2
			}
			if len(ns.ExternalToolRules) > 0 {
				fmt.Fprintf(stdout, "  external-tool rules re-scan on their own and may still reach this path: %s\n", strings.Join(ns.ExternalToolRules, ", "))
			}
			continue
		}
		if len(pr.Rules) == 0 {
			fmt.Fprintln(stdout, "  (none)")
			continue
		}
		for _, g := range pr.Rules {
			line := fmt.Sprintf("  %s\t%s\t%s", g.ID, g.Severity, g.Type)
			if g.SuppressedBy != "" {
				line += "  (suppressed: " + g.SuppressedBy + ")"
			}
			fmt.Fprintln(stdout, line)
			if g.Cure != "" {
				fmt.Fprintf(stdout, "    cure: %s\n", g.Cure)
			}
		}
	}
	return 0
}

// gitignoreUnanswerable is the ONE spelling of the declared-but-unanswerable
// refusal, shared by the snapshot resolution and the ghost re-ask so the two
// framings cannot drift. check may soften the same failure (pruning nothing
// only widens its scan); a governance answer may not.
func gitignoreUnanswerable(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "formwork: scan.gitignore is declared but git could not answer: %v (refusing to guess which paths the walk would prune)\n", err)
	return 2
}

// notScannedAnswer renders a hidden verdict as the displayed answer — one
// construction site for every channel, so the snapshot and ghost frames
// cannot drift apart. Every channel is an EXPLICIT arm: a verdict claiming a
// channel this switch does not know is an internal error, never a silent
// fall-through to the builtin-skip wording (#125 review — the next channel
// addition must fail closed here, not mislabel).
func notScannedAnswer(v scan.NotScannedVerdict, cfg *config.Config, externalTools []string) (*notScannedOut, error) {
	ns := &notScannedOut{ExternalToolRules: externalTools}
	switch {
	case v.Builtin:
		ns.By = "builtin-skip"
	case v.Glob != "":
		ns.By, ns.Glob = "scan.ignore", v.Glob
		for _, e := range cfg.Ignore {
			if e.Glob == v.Glob {
				ns.Reason = e.Reason
				break
			}
		}
	case v.GitRule != "":
		// The deciding .gitignore line travels in the same
		// <file>:<line>:<pattern> shape the census carries, with the
		// operator's declared reason for the channel itself.
		ns.By, ns.Rule, ns.Reason = "scan.gitignore", v.GitRule, cfg.Gitignore.Reason
	default:
		return nil, fmt.Errorf("internal: hidden path with no attributed channel (%+v)", v)
	}
	return ns, nil
}

// checkIgnored is vcs.CheckIgnored behind a seam so the batch-failure
// deferral (held ghostErr, git-free verdicts still answering) is testable —
// both git calls in one invocation happen milliseconds apart, so no test can
// flip real git state between them.
var checkIgnored = vcs.CheckIgnored

// underAny reports whether path equals or sits beneath any of the (sorted,
// slash-separated) prefixes.
func underAny(path string, prefixes []string) bool {
	for _, pre := range prefixes {
		if path == pre || strings.HasPrefix(path, pre+"/") {
			return true
		}
	}
	return false
}

// ghostOverlay builds the gitignore view for ONE ghost query, gated on git's
// leaf verdict: nil when git says the landed file would not be ignored (the
// gitignore channel then says nothing about this path, whatever any
// dir-frame string match or today's snapshot collapse claims); otherwise the
// leaf record plus the prune LEVEL — each ancestor's dir-frame record (ghost
// ancestors from the batch, existing ancestors from the snapshot) — so the
// shared interleave attributes at the level the future walk would prune,
// without any per-query record leaking into another query's view.
func ghostOverlay(p string, ghostFrom int, leafGhost, ghostDirs map[string]string, snapshot *scan.GitIgnored) *scan.GitIgnored {
	leafRule, ok := leafGhost[p]
	if !ok {
		return nil
	}
	overlay := scan.NewGitIgnored()
	segs := strings.Split(p, "/")
	for i := 0; i < len(segs)-1; i++ {
		prefix := strings.Join(segs[:i+1], "/")
		if i >= ghostFrom {
			if rule, ok := ghostDirs[prefix]; ok {
				overlay.Add(prefix, true, rule)
			}
		} else if rule, ok := snapshot.Lookup(prefix, true); ok {
			overlay.Add(prefix, true, rule)
		}
	}
	overlay.Add(p, false, leafRule)
	return overlay
}

// ghostCandidates lists p's GHOST segments (from index ghostFrom in p's
// slash-split segments) in both frames git must answer: each non-leaf ghost
// prefix as a directory (trailing slash tells check-ignore to apply dir-only
// patterns — used for prune-LEVEL refinement only, never to decide hiding),
// the leaf as a file (the deciding frame). Existing segments are
// deliberately NOT re-asked — the resolved snapshot already holds git's
// answer for them, including the collapse nuance an existing dir's tracked
// contents impose.
func ghostCandidates(p string, ghostFrom int) []string {
	segs := strings.Split(p, "/")
	candidates := make([]string, 0, len(segs)-ghostFrom)
	for i := ghostFrom; i < len(segs); i++ {
		prefix := strings.Join(segs[:i+1], "/")
		if i < len(segs)-1 {
			prefix += "/"
		}
		candidates = append(candidates, prefix)
	}
	return candidates
}

// queryNormalizer turns rules-for arguments into the root-relative,
// slash-separated frame Applies() judges — the same frame the scanner uses,
// so the displayed verdict can never disagree with the scan's. One instance
// serves a whole invocation: the case-insensitivity probe runs once and
// directory listings are cached, so an N-path query does not repeat
// identical I/O N times (#119 third-pass finding 6).
type queryNormalizer struct {
	root string
	fold bool
	dirs map[string][]os.DirEntry
}

func newQueryNormalizer(root string) *queryNormalizer {
	return &queryNormalizer{root: root, fold: fsCaseInsensitive(root), dirs: map[string][]os.DirEntry{}}
}

// normalize resolves one argument. Absolute paths under root are relativized
// — on a case-insensitive filesystem both the argument and the root are
// case-canonicalized FIRST, so a case-divergent absolute spelling of an
// in-root file is recognized as in-root rather than misdiagnosed as outside
// it (#119 third-pass finding 4). Anything genuinely outside root, or naming
// a directory (including the root itself), is an error, never an empty
// result.
func (n *queryNormalizer) normalize(arg string) (string, error) {
	p := arg
	if filepath.IsAbs(arg) {
		absRoot, err := filepath.Abs(n.root)
		if err != nil {
			return "", err
		}
		argAbs, rootAbs := path.Clean(filepath.ToSlash(arg)), path.Clean(filepath.ToSlash(absRoot))
		if n.fold {
			argAbs = n.canonicalizeAbs(argAbs)
			rootAbs = n.canonicalizeAbs(rootAbs)
		}
		rel, err := filepath.Rel(rootAbs, argAbs)
		if err != nil {
			return "", fmt.Errorf("path %s is outside the repository root %s", arg, absRoot)
		}
		p = rel
	}
	p = path.Clean(filepath.ToSlash(p))
	// "." IS the root — a directory, so it gets the directory diagnosis, not
	// a false "outside the root" (#119 review finding 10).
	if p == "." {
		return "", fmt.Errorf("path %q names a directory (the repository root); rules govern file paths — query a file (existing or intended)", arg)
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("path %q is outside the repository root (give a root-relative or absolute-under-root file path)", arg)
	}
	// On a case-insensitive filesystem a case-divergent query opens the real
	// file but would be judged in a frame the walk never uses — governed
	// files answering "(none)", ignored files answering with rules (#119
	// review finding 3). Canonicalize to the on-disk spelling there, and
	// ONLY there: on a case-sensitive filesystem the divergent spelling is
	// genuinely a different (future) file and must keep its queried frame.
	if n.fold {
		p = n.canonicalize(n.root, p)
	}
	// A directory query answered with the bare dir string would match no
	// file glob and read "(none)" — a trusted-empty fail-open, because every
	// file inside may be governed (fail-open review finding 1). Refuse both
	// the explicit shape (trailing slash) and the implicit one (an existing
	// directory) — via Lstat, because a symlink-to-directory is the walk's
	// NON-REGULAR skip, not a directory, and must reach that annotation
	// instead (#119 third-pass finding 5). A nonexistent path stays
	// legitimate: it cannot stat to a directory, and guidance is asked about
	// files not yet written.
	if strings.HasSuffix(filepath.ToSlash(arg), "/") {
		return "", fmt.Errorf("path %q names a directory; rules govern file paths — query a file (existing or intended)", arg)
	}
	if st, err := os.Lstat(filepath.Join(n.root, filepath.FromSlash(p))); err == nil && st.IsDir() {
		return "", fmt.Errorf("path %q is a directory; rules govern file paths — query a file (existing or intended)", arg)
	}
	return p, nil
}

// readDir lists dir through the per-invocation cache. Errors read as an
// empty listing here — canonicalization then keeps the queried spelling, and
// walkReachability owns loud classification of unreadable paths.
func (n *queryNormalizer) readDir(dir string) []os.DirEntry {
	if es, ok := n.dirs[dir]; ok {
		return es
	}
	es, _ := os.ReadDir(dir)
	n.dirs[dir] = es
	return es
}

// canonicalize rewrites each existing component of p (relative, slash-
// separated) to its on-disk spelling under base: exact-case match preferred,
// else the case-insensitive match. Components that do not exist — the ghost
// tail of a file about to be written — keep the queried spelling.
func (n *queryNormalizer) canonicalize(base, p string) string {
	cur := base
	segs := strings.Split(p, "/")
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		name := seg
		exact := false
		entries := n.readDir(cur)
		for _, e := range entries {
			if e.Name() == seg {
				exact = true
				break
			}
		}
		if !exact {
			for _, e := range entries {
				if strings.EqualFold(e.Name(), seg) {
					name = e.Name()
					break
				}
			}
		}
		out = append(out, name)
		cur = filepath.Join(cur, name)
	}
	return strings.Join(out, "/")
}

// canonicalizeAbs canonicalizes an absolute slash-path's spelling from the
// filesystem root down.
func (n *queryNormalizer) canonicalizeAbs(abs string) string {
	return "/" + n.canonicalize("/", strings.TrimPrefix(abs, "/"))
}

// fsCaseInsensitive probes whether root's filesystem folds case, read-only:
// .formwork must exist for any config-loaded command, so its case-toggled
// spelling resolving is the tell. (A repo carrying a literal .FORMWORK dir
// on a case-sensitive filesystem would false-positive this; canonicalization
// still prefers exact-case matches, so only genuinely case-divergent queries
// are affected in that pathological layout.)
func fsCaseInsensitive(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".FORMWORK"))
	return err == nil
}

// walkReachability classifies how the walk treats p's on-disk state, walking
// root-down exactly as the scanner would: nonRegular=true means some
// component is a non-regular entry the walk skips (symlink, fifo — at the
// leaf or an ANCESTOR: the walk skips the symlinked dir entry itself and
// never descends, #119 third-pass finding 1); ghostFrom is the index of the
// first segment that does not exist (-1 when the whole path does) — the
// legitimate future-file case, which the gitignore channel must re-ask git
// about because its prune snapshot holds only existing paths (#122 ghost
// frame; the index bounds the re-ask to exactly the ghost segments). Two
// shapes are errors, never confident answers: an ancestor that is a regular
// FILE (nothing can ever exist beneath it), and any stat failure other than
// not-exist — a path we cannot classify must not be answered (#119
// third-pass finding 3).
func walkReachability(root, p string) (nonRegular bool, ghostFrom int, err error) {
	segs := strings.Split(p, "/")
	cur := root
	for i, seg := range segs {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return false, i, nil // ghost tail: scannable once written
			}
			return false, -1, fmt.Errorf("cannot classify %s: %v", p, err)
		}
		if i == len(segs)-1 {
			return !fi.Mode().IsRegular(), -1, nil // directories were refused in normalize
		}
		if fi.IsDir() {
			continue
		}
		if fi.Mode().IsRegular() {
			return false, -1, fmt.Errorf("path %s cannot exist: ancestor %s is a regular file", p, strings.Join(segs[:i+1], "/"))
		}
		return true, -1, nil // non-regular ancestor: the walk never descends here
	}
	return false, -1, nil
}
