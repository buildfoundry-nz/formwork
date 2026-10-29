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

func runCheckContentBatch(cfg *config.Config, root string, stdin io.Reader, stdout, stderr io.Writer, allFindings bool) int {
	reader := bufio.NewReader(stdin)
	results := []checkContentDocument{}
	records := 0
	hasErrorFinding := false
	for lineNo := 1; ; lineNo++ {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			records++
			record, err := decodeCheckContentBatchRecord(line)
			if err != nil {
				fmt.Fprintf(stderr, "formwork: check-content batch line %d: %v\n", lineNo, err)
				return 2
			}
			doc, findings, err := evaluateCheckContent(cfg, root, record.Path, []byte(*record.Content), allFindings, time.Now())
			if err != nil {
				fmt.Fprintf(stderr, "formwork: check-content batch line %d: %v\n", lineNo, err)
				return 2
			}
			for _, f := range findings {
				if f.Severity == finding.SeverityError {
					hasErrorFinding = true
				}
			}
			results = append(results, doc)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			fmt.Fprintln(stderr, "formwork: reading batch stdin:", readErr)
			return 2
		}
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
