package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

const checkContentSchemaVersion = 1

var checkContentLoadGated = loadGated

type checkContentCoverage struct {
	RulesTotal int `json:"rules_total"`
	InScope    int `json:"in_scope"`
	Eligible   int `json:"eligible"`
	Evaluated  int `json:"evaluated"`
	Deferred   int `json:"deferred"`
	OutOfScope int `json:"out_of_scope"`
}

type checkContentNotEvaluated struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

type checkContentFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Cure     string `json:"cure"`
	Explain  string `json:"explain"`
}

type checkContentDocument struct {
	SchemaVersion    int                        `json:"schema_version"`
	EngineVersion    string                     `json:"engine_version"`
	CorpusHash       string                     `json:"corpus_hash"`
	Path             string                     `json:"path"`
	ContentHash      string                     `json:"content_hash"`
	Status           string                     `json:"status"`
	Coverage         checkContentCoverage       `json:"coverage"`
	EvaluatedRuleIDs []string                   `json:"evaluated_rule_ids"`
	NotEvaluated     []checkContentNotEvaluated `json:"not_evaluated"`
	Findings         []checkContentFinding      `json:"findings"`
	FindingsTotal    int                        `json:"findings_total"`
	FindingsOmitted  int                        `json:"findings_omitted"`
	ReplayHandle     string                     `json:"replay_handle"`
	ElapsedMS        int64                      `json:"elapsed_ms"`
	NotScanned       *notScannedOut             `json:"not_scanned,omitempty"`
}

func newCheckContentDocument(cfg *config.Config, path string, content []byte, started time.Time, notScanned *notScannedOut) checkContentDocument {
	contentHash := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	engineVersion := displayVersion()
	reachability := "scanned"
	if notScanned != nil {
		reachability = fmt.Sprintf("not_scanned by=%q glob=%q rule=%q", notScanned.By, notScanned.Glob, notScanned.Rule)
	}
	replay := sha256.Sum256([]byte("formwork-check-content-replay-v1\x00" + engineVersion + "\x00" + cfg.CorpusHash + "\x00" + path + "\x00" + contentHash + "\x00" + reachability))
	return checkContentDocument{
		SchemaVersion:    checkContentSchemaVersion,
		EngineVersion:    engineVersion,
		CorpusHash:       cfg.CorpusHash,
		Path:             path,
		ContentHash:      contentHash,
		Status:           "no_eligible_rules",
		Coverage:         checkContentCoverage{RulesTotal: len(cfg.Rules)},
		EvaluatedRuleIDs: []string{},
		NotEvaluated:     []checkContentNotEvaluated{},
		Findings:         []checkContentFinding{},
		ReplayHandle:     fmt.Sprintf("sha256:%x", replay),
		ElapsedMS:        time.Since(started).Milliseconds(),
	}
}

func runCheckContent(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	started := time.Now()
	fs, root := newFlagSet("check-content", "repository root whose configuration governs the supplied content", stderr)
	pathFlag := fs.String("path", "", "root-relative file path to evaluate (required with single-file stdin; the path need not exist)")
	allFindings := fs.Bool("all-findings", false, "return every finding instead of the first three")
	batch := fs.Bool("batch", false, "read one {path, content} JSON object per stdin line and emit one result per line")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || (*batch && *pathFlag != "") || (!*batch && *pathFlag == "") {
		if *batch {
			fmt.Fprintln(stderr, "usage: formwork check-content --batch [--all-findings] (one {path, content} JSON object per stdin line; --path is not accepted)")
		} else {
			fmt.Fprintln(stderr, "usage: formwork check-content [flags] --path <file> (complete file content on stdin; no path operands)")
		}
		return 2
	}
	cfg, ok := checkContentLoadGated(*root, stderr)
	if !ok {
		return 2
	}
	if *batch {
		return runCheckContentBatch(cfg, *root, stdin, stdout, stderr, *allFindings)
	}
	content, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintln(stderr, "formwork: reading stdin:", err)
		return 2
	}
	doc, findings, err := evaluateCheckContent(cfg, *root, *pathFlag, content, *allFindings, started)
	if err != nil {
		fmt.Fprintln(stderr, "formwork:", err)
		return 2
	}
	if code := emitJSON(stdout, stderr, doc); code != 0 {
		return code
	}
	for _, f := range findings {
		if f.Severity == finding.SeverityError {
			return 1
		}
	}
	return 0
}

func evaluateCheckContent(cfg *config.Config, root, path string, content []byte, allFindings bool, started time.Time) (checkContentDocument, []finding.Finding, error) {
	normalized, err := newQueryNormalizer(root).normalize(path)
	if err != nil {
		return checkContentDocument{}, nil, err
	}
	externalTools := []string{}
	for _, r := range cfg.Rules {
		if r.Cost() != rules.CostFast {
			externalTools = append(externalTools, r.ID)
		}
	}
	notScanned, err := checkContentReachability(root, cfg, normalized, externalTools)
	if err != nil {
		return checkContentDocument{}, nil, err
	}
	doc := newCheckContentDocument(cfg, normalized, content, started, notScanned)
	if notScanned != nil {
		doc.Status = "not_scanned"
		doc.NotScanned = notScanned
		doc.ElapsedMS = time.Since(started).Milliseconds()
		return doc, nil, nil
	}

	eligible, notEvaluated, inScope, deferred, outOfScope := planCheckContent(cfg.Rules, normalized, content)
	doc.NotEvaluated = notEvaluated
	doc.Coverage.InScope = inScope
	doc.Coverage.Eligible = len(eligible)
	doc.Coverage.Evaluated = len(eligible)
	doc.Coverage.Deferred = deferred
	doc.Coverage.OutOfScope = outOfScope
	for _, r := range eligible {
		doc.EvaluatedRuleIDs = append(doc.EvaluatedRuleIDs, r.ID)
	}
	var findings []finding.Finding
	if len(eligible) > 0 {
		fset := &scan.FileSet{Root: root, Repo: root, Files: []*scan.File{scan.NewMemFile(normalized, content)}}
		findings, err = engine.Run(eligible, fset, 1)
		if err != nil {
			return checkContentDocument{}, nil, err
		}
	}
	evaluatedFindings := findings
	doc.FindingsTotal = len(evaluatedFindings)
	doc.FindingsOmitted = max(0, len(evaluatedFindings)-3)
	if allFindings {
		doc.FindingsOmitted = 0
	} else if len(evaluatedFindings) > 3 {
		findings = evaluatedFindings[:3]
	}
	cures := map[string]string{}
	for _, r := range cfg.Rules {
		cures[r.ID] = r.Cure
	}
	for _, f := range findings {
		doc.Findings = append(doc.Findings, checkContentFinding{
			Rule: f.RuleID, Severity: string(f.Severity), Path: f.Path, Line: f.Line,
			Message: f.Message, Cure: cures[f.RuleID], Explain: "formwork explain " + f.RuleID,
		})
	}
	if doc.FindingsTotal > 0 {
		doc.Status = "findings"
	} else if len(eligible) > 0 {
		doc.Status = "clean"
	}
	doc.ElapsedMS = time.Since(started).Milliseconds()
	return doc, evaluatedFindings, nil
}

func planCheckContent(rls []*config.Rule, path string, content []byte) (eligible []*config.Rule, notEvaluated []checkContentNotEvaluated, inScope, deferred, outOfScope int) {
	notEvaluated = []checkContentNotEvaluated{}
	generated := false
	for _, r := range rls {
		if g := r.Generation(); g != nil {
			_, generated = g.Declares(content)
			break
		}
	}
	deferRule := func(r *config.Rule, reason string) {
		notEvaluated = append(notEvaluated, checkContentNotEvaluated{Rule: r.ID, Reason: reason})
		deferred++
	}
	for _, r := range rls {
		if !r.Applies(path) {
			outOfScope++
			continue
		}
		if generated && !r.JudgesGenerated() {
			notEvaluated = append(notEvaluated, checkContentNotEvaluated{Rule: r.ID, Reason: "generated_excluded"})
			outOfScope++
			continue
		}
		inScope++
		switch {
		case r.MinFiles() > 0:
			deferRule(r, "scope_min_files")
		case len(r.NotDeclaringEntries()) > 0:
			deferRule(r, "scope_not_declaring")
		case r.JudgesGenerated():
			deferRule(r, "generated_only_scope")
		case rules.HasFinalizer(r.Checker):
			deferRule(r, "finalizer")
		case r.WholeTreeInvariant():
			deferRule(r, "whole_tree_invariant")
		case !rules.IsContentOnly(r.Checker, path):
			deferRule(r, "content_only_not_supported")
		default:
			eligible = append(eligible, r)
		}
	}
	return eligible, notEvaluated, inScope, deferred, outOfScope
}

func checkContentReachability(root string, cfg *config.Config, path string, externalTools []string) (*notScannedOut, error) {
	gitIgnore := meta.ResolveGitIgnore(cfg, root)
	nonRegular, ghostFrom, reachErr := walkReachability(root, path)
	needsGit := false
	ghostErr := error(nil)
	candidates := []string{}
	if reachErr == nil && ghostFrom >= 0 && gitIgnore.State == meta.GitIgnoreOn {
		submodules, err := vcs.Submodules(root)
		if err != nil {
			ghostErr = err
		}
		for _, candidate := range ghostCandidates(path, ghostFrom) {
			if underAny(strings.TrimSuffix(candidate, "/"), submodules) {
				continue
			}
			needsGit = true
			candidates = append(candidates, candidate)
		}
	}
	leafGhost := map[string]string{}
	ghostDirs := map[string]string{}
	if len(candidates) > 0 && ghostErr == nil {
		records, err := checkIgnored(root, candidates)
		if err != nil {
			ghostErr = err
		} else {
			for _, record := range records {
				if record.Dir {
					ghostDirs[record.Path] = record.Rule()
				} else {
					leafGhost[record.Path] = record.Rule()
				}
			}
		}
	}
	view := gitIgnore.Set
	if ghostFrom >= 0 {
		view = ghostOverlay(path, ghostFrom, leafGhost, ghostDirs, gitIgnore.Set)
	}
	verdict := scan.NotScannedBy(path, scan.Opts{Ignore: cfg.IgnoreGlobs(), GitIgnored: view})
	if verdict.Hidden() {
		return notScannedAnswer(verdict, cfg, externalTools)
	}
	if gitIgnore.State == meta.GitIgnoreUnknown {
		return nil, fmt.Errorf("scan.gitignore is declared but git could not answer: %v (refusing to guess which paths the walk would prune)", gitIgnore.Err)
	}
	if needsGit && ghostErr != nil {
		return nil, fmt.Errorf("scan.gitignore is declared but git could not answer: %v (refusing to guess which paths the walk would prune)", ghostErr)
	}
	if reachErr != nil {
		return nil, reachErr
	}
	if nonRegular {
		return &notScannedOut{By: "non-regular", ExternalToolRules: externalTools}, nil
	}
	return nil, nil
}
