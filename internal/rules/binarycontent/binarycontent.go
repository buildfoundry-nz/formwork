// Package binarycontent implements the `binary-content` rule type (spec §5):
// a per-file guard against oversized or binary blobs in tracked files. It is a
// fast, per-file Checker — it reads content only, never shelling out.
package binarycontent

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// binarySniffLen is the prefix inspected for a NUL byte; the common heuristic
// treats a NUL in the first 8000 bytes as evidence a file is binary.
const binarySniffLen = 8000

type binaryContentParams struct {
	MaxBytes     *int `yaml:"max_bytes"`
	ForbidBinary bool `yaml:"forbid_binary"`
}

type binaryContent struct {
	maxBytes     *int // nil ⇒ no size check
	forbidBinary bool
}

func newBinaryContent(params *yaml.Node) (rules.Checker, error) {
	var p binaryContentParams
	if err := rules.DecodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.MaxBytes == nil && !p.ForbidBinary {
		return nil, errors.New("binary-content: at least one of params.max_bytes or params.forbid_binary must be set")
	}
	if p.MaxBytes != nil && *p.MaxBytes < 0 {
		return nil, fmt.Errorf("binary-content: params.max_bytes must be non-negative, got %d", *p.MaxBytes)
	}
	return &binaryContent{maxBytes: p.MaxBytes, forbidBinary: p.ForbidBinary}, nil
}

func (c *binaryContent) CheckFile(f *scan.File) ([]rules.Match, error) {
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	var matches []rules.Match
	if c.maxBytes != nil && len(content) > *c.maxBytes {
		matches = append(matches, rules.Match{
			Message: fmt.Sprintf("file size %d bytes exceeds max_bytes %d", len(content), *c.maxBytes),
		})
	}
	if c.forbidBinary && looksBinary(content) {
		matches = append(matches, rules.Match{
			Message: fmt.Sprintf("file appears to be binary: NUL byte within first %d bytes", binarySniffLen),
		})
	}
	return matches, nil
}

// looksBinary reports whether content has a NUL byte in its sniff prefix.
func looksBinary(content []byte) bool {
	n := len(content)
	if n > binarySniffLen {
		n = binarySniffLen
	}
	return bytes.IndexByte(content[:n], 0) >= 0
}

func init() {
	rules.Register("binary-content", newBinaryContent)
}
