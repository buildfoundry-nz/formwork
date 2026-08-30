package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
)

// ValidFormat reports whether format names a renderer, without rendering
// anything. It exists so a caller can reject a mistyped -format up front,
// beside its other flag validation, instead of discovering it here after a
// whole scan has run — by which point the operator has waited for work whose
// output cannot be printed, and (once the caller grew other config-level
// refusals) may be told about their config instead of their typo.
//
// ADDING A RENDERER MEANS EDITING BOTH THIS LIST AND Render's SWITCH. The first
// cut of this comment said "adding a renderer means adding one case below and
// nothing else", which was an instruction into a silent fail: a format accepted
// here but unhandled there rendered zero bytes and returned nil, so a run with
// violations printed nothing. Render keeps a default: arm precisely so that
// mistake is loud instead.
func ValidFormat(format string) error {
	switch format {
	case "", "human", "json", "github":
		return nil
	}
	return fmt.Errorf("unknown format %q (want human, json, or github)", format)
}

// Render writes findings in the named format (spec §6): "human" (default),
// "json", or "github". An unknown format is an error (the caller exits 2).
//
// scan travels to every renderer, by value, so that no format can be the one
// that drops it (#151). -format github used to write zero bytes for a run with
// no findings, which made it the surface where a scan that looked at nothing
// was most invisible and the surface adopters most often read.
func Render(format string, w io.Writer, rls []*config.Rule, findings []finding.Finding, scan ScanSummary) error {
	if err := ValidFormat(format); err != nil {
		return err
	}
	switch format {
	case "", "human":
		Human(w, rls, findings, scan)
	case "json":
		JSON(w, rls, findings, scan)
	case "github":
		GitHub(w, rls, findings, scan)
	default:
		// Unreachable while this switch and ValidFormat agree, and kept for the
		// moment they do not: a format this switch cannot render must be an
		// error, never zero bytes and a nil return. Rendering nothing would let
		// a run WITH violations print an empty report.
		return fmt.Errorf("format %q passed validation but has no renderer — ValidFormat and Render have diverged", format)
	}
	return nil
}

// rulesPassed counts rules with no error-severity live findings (warn-only and
// clean rules both pass), matching the human summary.
func rulesPassed(rls []*config.Rule, live []finding.Finding) int {
	byRule := map[string][]finding.Finding{}
	for _, f := range live {
		byRule[f.RuleID] = append(byRule[f.RuleID], f)
	}
	passed := 0
	for _, r := range rls {
		if fs := byRule[r.ID]; len(fs) == 0 || !hasError(fs) {
			passed++
		}
	}
	return passed
}

type jsonFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
	Cure     string `json:"cure,omitempty"` // the rule's cure:, joined at render time (#107)
}

// curesByRule maps rule id → cure text, so both machine formats join cure at
// render time exactly as the human renderer does — cure stays on the rule,
// never threaded onto findings.
func curesByRule(rls []*config.Rule) map[string]string {
	cures := make(map[string]string, len(rls))
	for _, r := range rls {
		if r.Cure != "" {
			cures[r.ID] = r.Cure
		}
	}
	return cures
}

// jsonSuppressed names one suppressed finding (#91): what was exempted and
// through which channel ("marker" or "allowlist:<file>:<line>"), so the
// exemption surface is auditable from check output, not just countable.
type jsonSuppressed struct {
	Rule         string `json:"rule"`
	Severity     string `json:"severity"`
	Path         string `json:"path,omitempty"`
	Line         int    `json:"line,omitempty"`
	Message      string `json:"message"`
	SuppressedBy string `json:"suppressed_by"`
}

type jsonSummary struct {
	RulesTotal  int `json:"rules_total"`
	RulesPassed int `json:"rules_passed"`
	Findings    int `json:"findings"`
	Suppressed  int `json:"suppressed"`
}

type jsonReport struct {
	Findings   []jsonFinding    `json:"findings"`
	Suppressed []jsonSuppressed `json:"suppressed"`
	Scan       jsonScan         `json:"scan"`
	Summary    jsonSummary      `json:"summary"`
}

// JSON emits a stable machine-readable report: the live (unsuppressed)
// findings, the suppressed findings enumerated, and a summary whose
// suppressed count is the length of that enumeration — derived, not asserted
// alongside, so the two cannot drift (#91). Findings arrive engine-sorted, so
// output is deterministic.
//
// rls must be the complete loaded rule set the findings were produced from:
// each live finding's cure joins from it by rule id (#107), so a nil or
// filtered rls silently drops declared cures (and skews rules_total/passed).
func JSON(w io.Writer, rls []*config.Rule, findings []finding.Finding, scan ScanSummary) {
	live := finding.Unsuppressed(findings)
	rep := jsonReport{
		Findings:   make([]jsonFinding, 0, len(live)),
		Suppressed: make([]jsonSuppressed, 0, len(findings)-len(live)), // non-nil: empty encodes []
	}
	cures := curesByRule(rls)
	for _, f := range live {
		rep.Findings = append(rep.Findings, jsonFinding{
			Rule: f.RuleID, Severity: string(f.Severity), Path: f.Path, Line: f.Line,
			Message: f.Message, Cure: cures[f.RuleID],
		})
	}
	for _, f := range findings {
		if !f.Suppressed {
			continue
		}
		rep.Suppressed = append(rep.Suppressed, jsonSuppressed{
			Rule: f.RuleID, Severity: string(f.Severity), Path: f.Path, Line: f.Line,
			Message: f.Message, SuppressedBy: f.SuppressedBy,
		})
	}
	rep.Scan = scan.toJSON()
	rep.Summary = jsonSummary{
		RulesTotal:  len(rls),
		RulesPassed: rulesPassed(rls, live),
		Findings:    len(rep.Findings),
		Suppressed:  len(rep.Suppressed),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rep) // io.Writer errors surface to the caller's writer
}

// GitHub emits one workflow-command annotation per live finding
// (`::error file=…,line=…::message`), so violations show inline on a PR —
// then one `::notice` per suppressed finding (#91), naming what was exempted
// and through which channel, after the live annotations because reviewers
// read failures first. Notice level can never read as a failure and never
// affects the exit code (renderers are post-verdict). A live annotation whose
// rule declares cure: carries it appended to the message (#107) — workflow
// commands have no extra structured field, so the cure travels as message
// data, escaped like the rest of it; suppressed notices don't carry cure,
// because an exempted finding is not asking to be remediated.
//
// rls must be the complete loaded rule set the findings were produced from
// (it was ignored before #107): each annotation's cure joins from it by rule
// id, so a nil or filtered rls silently drops declared cures.
func GitHub(w io.Writer, rls []*config.Rule, findings []finding.Finding, scan ScanSummary) {
	cures := curesByRule(rls)
	for _, f := range finding.Unsuppressed(findings) {
		level := "error"
		if f.Severity == finding.SeverityWarn {
			level = "warning"
		}
		prefix := ghPrefix(level, f)
		msg := ghEscapeData(f.Message) + " (" + f.RuleID + ")"
		if cure := cures[f.RuleID]; cure != "" {
			msg = ghAppendCure(len(prefix), msg, f.RuleID, cure)
		}
		fmt.Fprintf(w, "%s%s\n", prefix, msg)
	}
	for _, f := range findings {
		if !f.Suppressed {
			continue
		}
		annotate(w, "notice", f, "suppressed: "+ghEscapeData(f.Message)+" ("+f.RuleID+"; "+f.SuppressedBy+")")
	}
	// The scan summary, last: reviewers read failures first, and this block is
	// context for them rather than a competitor. It carries no file= prop —
	// these are statements about the run, not about a location — and it is
	// emitted even when everything above it was empty, which is the whole
	// point. Notice level can never read as a failure and never affects the
	// exit code.
	fmt.Fprintf(w, "::notice::formwork: %s\n", ghEscapeData(scan.headline()))
	for _, l := range scan.details() {
		fmt.Fprintf(w, "::notice::formwork: %s\n", ghEscapeData(l))
	}
}

const (
	// ghLineBudget caps one emitted annotation line (prefix included, sans
	// trailing newline): GitHub truncates workflow-command lines past ~4096
	// chars SILENTLY — a long cure would vanish mid-sentence with no signal.
	ghLineBudget = 4096
	// ghMinCure is the smallest escaped-cure fragment worth emitting. Below
	// it the cure — and the marker — are omitted entirely: a 1–3 char sliver
	// helps nobody, and a marker appended without room of its own would risk
	// the very cap it exists to announce.
	ghMinCure = 48
)

// ghAppendCure appends a rule's cure to an already-escaped annotation
// message, keeping the whole emitted line (prefixLen + message data) within
// ghLineBudget. The finding message is NEVER modified. In order:
//
//  1. The full escaped cure fits within the budget → plain append, no marker.
//     (Checked first: when it holds, truncation arithmetic is meaningless —
//     avail can exceed the cure's own length.)
//  2. At least ghMinCure bytes of cure fit alongside the truncation marker →
//     append the cut fragment (backed off so it never ends inside a %XX
//     escape or a multi-byte rune) plus a marker pointing at
//     `formwork explain <rule-id>`, where the full cure lives.
//  3. Otherwise nothing is appended — no joiner, no marker. The message
//     alone stands, whatever its own length.
func ghAppendCure(prefixLen int, msg, ruleID, cure string) string {
	const joiner = "%0ACure: "
	escaped := ghEscapeData(cure)
	if prefixLen+len(msg)+len(joiner)+len(escaped) <= ghLineBudget {
		return msg + joiner + escaped
	}
	marker := ghEscapeData("… (cure truncated; run formwork explain " + ruleID + ")")
	avail := ghLineBudget - prefixLen - len(msg) - len(joiner) - len(marker)
	if avail < ghMinCure {
		return msg
	}
	// avail >= ghMinCure > 0, and past the full-fit check len(escaped) >
	// avail+len(marker), so this slice is always in range.
	cut := escaped[:avail]
	// Never end inside an escape sequence: every '%' in escaped opens a
	// 3-byte %XX, so a '%' in the final two bytes marks a severed one — back
	// off to just before it. Bounds are checked per step; no index here can
	// go negative.
	for back := 1; back <= 2 && back <= len(cut); back++ {
		if cut[len(cut)-back] == '%' {
			cut = cut[:len(cut)-back]
			break
		}
	}
	// Nor inside a multi-byte rune split by the byte-wise cut. At most
	// utf8.UTFMax-1 trailing bytes can belong to a severed rune, and
	// DecodeLastRuneInString is O(1) per step — never a whole-fragment
	// revalidation per trimmed byte, and structurally no floorless erosion:
	// an invalid byte deeper in the cure cannot drain the fragment.
	for i := 0; i < utf8.UTFMax-1; i++ {
		r, size := utf8.DecodeLastRuneInString(cut)
		if r != utf8.RuneError || size != 1 {
			break // clean boundary (size==0 means cut is already empty)
		}
		cut = cut[:len(cut)-1]
	}
	// Backoff may have eaten the margin: a fragment now below ghMinCure is
	// omitted under the same rule as an undersized avail — the message alone
	// stands, no joiner, no marker.
	if len(cut) < ghMinCure {
		return msg
	}
	return msg + joiner + cut + marker
}

// ghPrefix renders the workflow-command prefix for one finding, degrading
// file/line props as the finding's location allows. Its length is charged
// against ghLineBudget when a cure is appended.
func ghPrefix(level string, f finding.Finding) string {
	switch {
	case f.Path == "":
		return fmt.Sprintf("::%s::", level)
	case f.Line == 0:
		return fmt.Sprintf("::%s file=%s::", level, ghEscapeProp(f.Path))
	default:
		return fmt.Sprintf("::%s file=%s,line=%d::", level, ghEscapeProp(f.Path), f.Line)
	}
}

// annotate writes one workflow command.
func annotate(w io.Writer, level string, f finding.Finding, msg string) {
	fmt.Fprintf(w, "%s%s\n", ghPrefix(level, f), msg)
}

// ghEscapeData escapes the message body of a workflow command.
func ghEscapeData(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// ghEscapeProp escapes a command property value (stricter than data).
func ghEscapeProp(s string) string {
	s = ghEscapeData(s)
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, ",", "%2C")
	return s
}
