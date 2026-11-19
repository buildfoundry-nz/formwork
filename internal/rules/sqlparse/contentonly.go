package sqlparse

import "github.com/buildfoundry-nz/formwork/internal/sqlextract"

func sqlFileOnly(path string) bool { return sqlextract.FileKind(path) == "sql" }

func (*parses) ContentOnly(path string) bool        { return sqlFileOnly(path) }
func (*lockingTarget) ContentOnly(path string) bool { return sqlFileOnly(path) }

// ContentOnly is false whenever the rule reads params.schemas: its verdict
// then depends on schema files beside the one being judged, and a
// single-content check has no tree to read them from.
func (c *lockingOrder) ContentOnly(path string) bool {
	return sqlFileOnly(path) && c.schema == nil
}
