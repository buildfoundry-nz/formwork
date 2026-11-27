package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
)

type checkContentBatchRecord struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

func decodeCheckContentBatchRecord(line []byte) (checkContentBatchRecord, error) {
	var record checkContentBatchRecord
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&record); err != nil {
		return record, fmt.Errorf("decoding JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return record, fmt.Errorf("multiple JSON values on one line")
		}
		return record, fmt.Errorf("decoding trailing JSON: %w", err)
	}
	if record.Path == "" {
		return record, fmt.Errorf("path is required")
	}
	if record.Content == nil {
		return record, fmt.Errorf("content is required (use an empty string for empty content)")
	}
	return record, nil
}

// runCheckContentBatch reads every record before it evaluates any, so the
// gitignore snapshot — a property of the worktree, not of a record — is resolved
// once for all of their paths. What it reports, and in which order, is what
// evaluating each line as it arrived reported: records are judged in input
// order, the first one that fails is the error, and a line that does not decode
// (or a read that fails) is reported only once every record before it has been
// judged.
func runCheckContentBatch(cfg *config.Config, root string, stdin io.Reader, stdout, stderr io.Writer, allFindings bool) int {
	type numbered struct {
		lineNo int
		record checkContentBatchRecord
	}
	reader := bufio.NewReader(stdin)
	var pending []numbered
	var held string // the decode or read failure that ends the input, if any
	records := 0
	for lineNo := 1; ; lineNo++ {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			records++
			record, err := decodeCheckContentBatchRecord(line)
			if err != nil {
				held = fmt.Sprintf("formwork: check-content batch line %d: %v", lineNo, err)
				break
			}
			pending = append(pending, numbered{lineNo, record})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			held = fmt.Sprint("formwork: reading batch stdin: ", readErr)
			break
		}
	}
	paths := make([]string, len(pending))
	for i, p := range pending {
		paths[i] = p.record.Path
	}
	gitIgnore := checkContentResolveGitIgnore(cfg, root, normalizedPaths(root, paths))
	results := []checkContentDocument{}
	hasErrorFinding := false
	for _, p := range pending {
		doc, findings, err := evaluateCheckContent(cfg, gitIgnore, root, p.record.Path, []byte(*p.record.Content), allFindings, time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "formwork: check-content batch line %d: %v\n", p.lineNo, err)
			return 2
		}
		for _, f := range findings {
			if f.Severity == finding.SeverityError {
				hasErrorFinding = true
			}
		}
		results = append(results, doc)
	}
	if held != "" {
		fmt.Fprintln(stderr, held)
		return 2
	}
	if records == 0 {
		fmt.Fprintln(stderr, "formwork: check-content --batch requires at least one JSON record; an empty batch checks nothing")
		return 2
	}
	for _, doc := range results {
		if code := emitJSONLine(stdout, stderr, doc); code != 0 {
			return code
		}
	}
	if hasErrorFinding {
		return 1
	}
	return 0
}
