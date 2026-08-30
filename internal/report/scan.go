// scan.go — the scan summary (#151 rows 1-8, 13). Split from format.go, which
// the 750-line vendor cap bounds; same package.
package report

import "fmt"

// ScanSummary is what a run reports about the SCAN, as opposed to about the
// findings: how much it looked at, which rules had nothing to look at, and what
// each declared prune channel removed from the walk.
//
// It exists because "0 finding(s)" answers only half the question an operator
// is asking. A rule that matched no files and a rule that matched files and
// found nothing both render `[id] OK`, and a scan that saw zero files renders
// the same summary line as a clean tree — so `check` could not distinguish a
// green repo from one it never looked at (#151).
//
// Every renderer emits it. The value type makes the ARGUMENT non-omittable at
// the call site; what makes the OUTPUT non-omittable is a test per format —
// TestHumanZeroRules, TestGitHubEmitsScanSummaryWithNoFindings and
// TestCheckScanSummaryReachesJSON. An optional disclosure is one an adopter's
// CI does not have.
type ScanSummary struct {
	// FilesScanned is how many files the rules were evaluated over. Under
	// --staged/--range that is the restricted set, which is what those flags
	// asked about.
	FilesScanned int
	// PathsRequested is how many paths git NAMED under --staged/--range, and is
	// 0 in a whole-tree run (FileSetMode is the discriminator, not this).
	//
	// It exists because the difference between the two counts is the only thing
	// that separates "you asked for nothing" from "you asked for paths and I
	// could not see any of them" — and those rendered identically before.
	//
	// WHAT STILL REACHES THE GAP is now narrow, and the list that used to be here
	// named three defects by number, which was the wrong frame twice over — an
	// issue's state is not a fact about this struct, and two of the three had
	// moved on. Stated as shapes instead:
	//
	//   - A path git named that the scan did not produce is now REFUSED at exit
	//     2 by #158's fix, before any summary renders. Whatever the cause — a
	//     spelling mismatch between git's name and the on-disk one included — it
	//     no longer arrives here to be counted.
	//   - A pathspec that yields no paths at all produces no gap: both counts
	//     are 0, and there is nothing to differ.
	//
	// What is left is the deliberate carve-outs — a symlink or a submodule
	// gitlink git named, which the walk produces for nobody and which the run is
	// right not to refuse. That is why
	// TestCheckStagedDistinguishesRequestedFromScanned had to be rebuilt around
	// a symlink: it is the only construction that still reaches this state.
	PathsRequested int
	// FileSetMode names the flag that restricted the run ("--staged", "--range
	// <spec>"), empty for a whole-tree run.
	FileSetMode string
	// InvariantRules counts rules that BYPASSED the changeset entirely: a
	// whole-repo invariant is non-monotonic under file removal, so it evaluates
	// over the tracked tree even in a file-set run (#4). Reporting the
	// changeset's size beside "N/N rules passed" without saying so is a
	// manufactured coverage claim — those rules did not read those files.
	InvariantRules int
	// RulesMatchingNoFiles names the rules whose scope selected nothing out of
	// what was scanned, in the order the rules were given. External-tool rules
	// are absent (see meta.RulesMatchingNoFiles for why).
	//
	// It is populated only for a WHOLE-TREE run. Vacuity is a whole-tree
	// question: under --staged a rule that does not cover this commit is not
	// vacuous, it is merely irrelevant to it, and naming every such rule would
	// bury the real signal on the path that runs most — the pre-commit shim.
	RulesMatchingNoFiles []string
	// RulesNotRun names the rules that reached no verdict at all, with the reason,
	// in the order they were given. Two channels feed it and the reason says
	// which: a CHECKER that declined to run itself (`command`'s
	// when.paths_changed, whose tool never ran), and a rule the CLI filtered out
	// before the engine (--skip-escapes). Both rendered `[id] OK` or nothing at
	// all — indistinguishable from a rule whose tool ran and passed (#159).
	//
	// One list rather than one per channel, because the operator's question is
	// "which rules did not run, and why", and answering it from two places is how
	// two commands came to describe the same state differently (#151).
	//
	// Unlike RulesMatchingNoFiles this is populated in EVERY mode, whole-tree and
	// file-set alike, and the difference is deliberate. Vacuity is a whole-tree
	// question — under --staged a rule that does not cover the commit is
	// irrelevant, not vacuous. A rule that did not run is not: a file-set run is
	// where a paths_changed trigger goes unmatched, so suppressing it there would
	// suppress it where it fires.
	RulesNotRun []SkippedRule
	// Prunes enumerates the DECLARED prune channels with live counts. Only
	// scan.ignore and scan.gitignore appear, because a channel here is built
	// per config entry and those are the only two an operator declares.
	// Built-in skips, scope.exclude and except.paths are NOT here: the first is
	// never recorded by the walk, and the last two are rule fields the scan
	// package never sees — which is why RulesMatchingNoFiles is computed from
	// the scope predicate rather than derived from this census.
	//
	// Nor are unfollowed symlinks, and that one is not an omission — see
	// UnfollowedLinks below, which is their peer field. (Before #235 they
	// really were unrecorded, and this comment said so; the walk has recorded
	// them since, so the sentence that grouped them with the built-in skips has
	// been corrected rather than carried forward.)
	Prunes []PruneChannel
	// UnfollowedLinks names every symlink the WALK declined to follow, sorted
	// by path. It is a PEER of Prunes and deliberately not a member of it: a
	// PruneChannel is one thing the operator DECLARED and what it removed, and
	// an unfollowed link is declared nowhere — no glob, no .gitignore line — so
	// there is nothing for a channel to key on. meta.PruneChannels says the
	// same from the producing side.
	//
	// That absence of a declaration is exactly why the list is owed. The walk
	// skips such a link rather than refusing it (#143 chose the third option
	// for a name no toolchain compiles), and until #309 the record reached only
	// `formwork lint` — so `check` reported `0 finding(s)` at exit 0 over a
	// path it never opened, with every vacuity indicator empty. Reporting what
	// a run did NOT look at is half of what this type is for.
	//
	// It changes no verdict. The walk follows no symlink in any mode, and this
	// is the disclosure of that fact, not a new gate in front of it.
	UnfollowedLinks []string
}

// UnfollowedLinkLine renders one unfollowed-symlink disclosure. It is the
// single formatter for these lines, so `check`'s scan summary and `lint`'s
// escape-hatch enumeration cannot describe the same skip in different words —
// the same reason PruneChannel and SkippedRule own their own Line.
//
// The reason is carried by the formatter rather than by the value, because it
// is invariant: every entry here is skipped for the one reason that formwork
// never follows a link. A per-entry reason field would be a slot for a
// divergence that cannot exist.
func UnfollowedLinkLine(path string) string {
	return "scan: symlink not followed: " + path + " (skipped, and nothing under it scanned — formwork never follows links)"
}

// Skip channels. Two things stop a rule running, and they are different in kind:
// a checker declining its own gate is the rule behaving as configured, while
// --skip-escapes is an operator narrowing this run. A CI that wants to alert on
// the second and not the first must be able to tell them apart without
// string-matching prose, which is why PruneChannel below carries a Channel too.
//
// SkipChannelSelf is gate-agnostic on purpose. The code that sets it,
// meta.SelfSkippedRules, asks the generic rules.SkipReporter interface, and that
// interface does not say WHICH gate a checker took — so a trigger-specific value
// like "when-trigger" would be a claim the producing code cannot support, true
// only for as long as `command`'s when.paths_changed happens to be the sole
// implementor. What this channel can back is the distinction that matters to a
// consumer: the rule declined itself, versus an operator narrowed the run. The
// gate specifics live in the reason text, where the checker that knows them
// writes them.
const (
	SkipChannelSelf        = "self-skip"
	SkipChannelSkipEscapes = "skip-escapes"
)

// SkippedRule is one rule that did not run, and why. Reason is the explanation
// its channel supplies, rendered verbatim: report owns the layout, the channel
// owns the words, since only it knows what stopped the rule. Channel is that
// same fact structurally, for machine consumers.
type SkippedRule struct {
	RuleID  string
	Channel string
	Reason  string
}

// Line renders one entry. It is the single formatter for these lines, so the
// human and github renderers cannot describe the same skip differently — the
// same reason PruneChannel below owns its own Line.
//
// An empty reason still produces a line: a rule that did not run without
// explaining itself must be visible as such rather than absent, which is the
// whole failure this type exists to remove.
func (s SkippedRule) Line() string {
	if s.Reason == "" {
		return s.RuleID + ": did not run, and no reason was recorded"
	}
	return s.RuleID + ": " + s.Reason
}

// PruneChannel is one declared prune channel and what it removed. Dirs counts
// pruned directories rather than the files under them: the walk did not
// descend, so a file count would have to be invented at the cost of what the
// prune saved (#56).
type PruneChannel struct {
	Channel string // "scan.ignore" or "scan.gitignore"
	Glob    string // the declared glob; empty for scan.gitignore, which has none
	Reason  string // the operator's declared reason
	Dirs    int
	Files   int
	// Undetermined carries git's failure when scan.gitignore was declared but
	// could not be resolved. It is reported in words, never as a count:
	// "0 matches" asserts that git was asked and said none, and a run where git
	// could not answer must not be able to borrow that sentence.
	Undetermined string
}

// Line renders one channel. It is the single formatter for these lines: both
// `check`'s scan summary and `lint`'s escape-hatch enumeration call it, so the
// two commands cannot describe the same channel differently — which is the
// failure mode #151 is about one level up.
func (p PruneChannel) Line() string {
	name := p.Channel
	if p.Glob != "" {
		name += ": " + p.Glob + " —"
	} else {
		name += ":"
	}
	switch {
	case p.Undetermined != "":
		return fmt.Sprintf("%s could not determine — %s; nothing pruned (%s)", name, p.Undetermined, p.Reason)
	case p.Dirs+p.Files == 0:
		return fmt.Sprintf("%s 0 matches (%s)", name, p.Reason)
	default:
		return fmt.Sprintf("%s %d dirs pruned (subtrees not scanned), %d files ignored (%s)", name, p.Dirs, p.Files, p.Reason)
	}
}

// headline is the one line every run emits, whatever else it has to say. Under
// a file-set mode it reports BOTH counts, because one of them alone cannot
// distinguish an empty request from an unseen one.
func (s ScanSummary) headline() string {
	if s.FileSetMode == "" {
		return fmt.Sprintf("%d file(s) scanned", s.FilesScanned)
	}
	return fmt.Sprintf("%d path(s) requested by %s, %d file(s) scanned", s.PathsRequested, s.FileSetMode, s.FilesScanned)
}

// detailListCap bounds how many rules each per-rule list — vacuous, then
// not-run — names in the LINE-ORIENTED renderers. A mid-port corpus
// legitimately has hundreds of vacuous rules — 572 of the 707 rules in
// examples/palletra-port-full — and an unbounded list puts a wall of text
// between the findings and the verdict line, burying both. JSON is uncapped:
// a machine consumer has no readability problem, and handing it a silent prefix
// of the list it asked for is exactly the quiet answer this type exists to
// remove.
//
// One constant for both lists, because the readability budget is the block's,
// not either list's. The two overflow lines differ, though: they have to point
// at somewhere the dropped names can actually be read.
const detailListCap = 10

// details returns the conditional lines: a rule that could not fire, and each
// declared channel. Unconditional lines would put "no rules matched nothing" in
// front of every clean run, which trains readers to skip the block.
//
// The cap is DISCLOSED, never silent: the overflow line states the exact number
// dropped and where the rest is, because a truncated list that does not say so
// reads as "that was all of them".
func (s ScanSummary) details() []string {
	out := make([]string, 0, len(s.RulesMatchingNoFiles)+len(s.RulesNotRun)+len(s.Prunes)+3)
	shown := s.RulesMatchingNoFiles
	if len(shown) > detailListCap {
		shown = shown[:detailListCap]
	}
	for _, id := range shown {
		out = append(out, id+": scope matched no files")
	}
	if dropped := len(s.RulesMatchingNoFiles) - len(shown); dropped > 0 {
		// The referral is true only because this list is whole-tree-only: lint
		// judges vacuity against the whole tree, so pointing a --staged reader
		// at it would send them to a command with nothing to say.
		out = append(out, fmt.Sprintf("… and %d more rule(s) matched no files (formwork lint names every one)", dropped))
	}
	// The rules that did NOT RUN, after the ones that had nothing to look at:
	// both are "this rule reached no verdict", and a reader scanning for that
	// finds them together.
	skips := s.RulesNotRun
	if len(skips) > detailListCap {
		skips = skips[:detailListCap]
	}
	for _, sk := range skips {
		out = append(out, sk.Line())
	}
	if dropped := len(s.RulesNotRun) - len(skips); dropped > 0 {
		// NOT a referral to lint, unlike the vacuous overflow above. Not because
		// lint runs no checkers — it does, over its own selection of them — but
		// because lint renders no scan summary and reports no skip at all, and
		// what it does run is a different selection of rules, so its answer would
		// not be this run's answer even if it had one. -format json is where the
		// rest of this list exists, and toJSON below emits it uncapped.
		out = append(out, fmt.Sprintf("… and %d more rule(s) did not run (-format json names every one)", dropped))
	}
	if s.InvariantRules > 0 {
		out = append(out, fmt.Sprintf("%d whole-tree invariant rule(s) evaluated over the tracked tree, not the changeset", s.InvariantRules))
	}
	for _, p := range s.Prunes {
		out = append(out, p.Line())
	}
	// The UNDECLARED skips last, under the declared ones: both answer "what did
	// this run not look at", and a reader who has just read the channels they
	// wrote is in the right frame for the one they did not.
	links := s.UnfollowedLinks
	if len(links) > detailListCap {
		links = links[:detailListCap]
	}
	for _, p := range links {
		out = append(out, UnfollowedLinkLine(p))
	}
	if dropped := len(s.UnfollowedLinks) - len(links); dropped > 0 {
		// Disclosed, like the two lists above, and pointed at a surface that
		// really does hold the rest: toJSON emits this list uncapped.
		out = append(out, fmt.Sprintf("… and %d more symlink(s) not followed (-format json names every one)", dropped))
	}
	return out
}

type jsonPrune struct {
	Channel      string `json:"channel"`
	Glob         string `json:"glob,omitempty"`
	Reason       string `json:"reason"`
	Dirs         int    `json:"dirs_pruned"`
	Files        int    `json:"files_ignored"`
	Undetermined string `json:"undetermined,omitempty"`
}

type jsonSkip struct {
	Rule    string `json:"rule"`
	Channel string `json:"channel"`
	Reason  string `json:"reason"`
}

type jsonScan struct {
	FilesScanned         int         `json:"files_scanned"`
	PathsRequested       int         `json:"paths_requested"`
	FileSetMode          string      `json:"file_set_mode,omitempty"`
	InvariantRules       int         `json:"invariant_rules"`
	RulesMatchingNoFiles []string    `json:"rules_matching_no_files"`
	NotRun               []jsonSkip  `json:"rules_not_run"`
	Prunes               []jsonPrune `json:"prune_channels"`
	UnfollowedLinks      []string    `json:"unfollowed_symlinks"`
}

// toJSON renders the summary with non-nil slices, so an empty one encodes as
// [] rather than null — a consumer distinguishing "none" from "absent" should
// not have to.
func (s ScanSummary) toJSON() jsonScan {
	out := jsonScan{
		FilesScanned:         s.FilesScanned,
		PathsRequested:       s.PathsRequested,
		FileSetMode:          s.FileSetMode,
		InvariantRules:       s.InvariantRules,
		RulesMatchingNoFiles: make([]string, 0, len(s.RulesMatchingNoFiles)),
		NotRun:               make([]jsonSkip, 0, len(s.RulesNotRun)),
		Prunes:               make([]jsonPrune, 0, len(s.Prunes)),
		UnfollowedLinks:      make([]string, 0, len(s.UnfollowedLinks)),
	}
	out.RulesMatchingNoFiles = append(out.RulesMatchingNoFiles, s.RulesMatchingNoFiles...)
	// Uncapped, for the reason the not-run list is: the cap is a readability
	// measure for a person, and handing a machine a silent prefix of the list it
	// asked for is the quiet answer this type exists to remove.
	out.UnfollowedLinks = append(out.UnfollowedLinks, s.UnfollowedLinks...)
	// Uncapped, where the line renderers cap at detailListCap: the reason travels
	// as its own field rather than as a rendered line, so a consumer can match on
	// the rule id without parsing prose.
	for _, sk := range s.RulesNotRun {
		out.NotRun = append(out.NotRun, jsonSkip{Rule: sk.RuleID, Channel: sk.Channel, Reason: sk.Reason})
	}
	for _, p := range s.Prunes {
		out.Prunes = append(out.Prunes, jsonPrune{
			Channel: p.Channel, Glob: p.Glob, Reason: p.Reason,
			Dirs: p.Dirs, Files: p.Files, Undetermined: p.Undetermined,
		})
	}
	return out
}
