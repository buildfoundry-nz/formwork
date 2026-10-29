package sqlparse

import "github.com/buildfoundry-nz/formwork/internal/sqlextract"

func sqlFileOnly(path string) bool { return sqlextract.FileKind(path) == "sql" }

func (*parses) ContentOnly(path string) bool        { return sqlFileOnly(path) }
func (*lockingOrder) ContentOnly(path string) bool  { return sqlFileOnly(path) }
func (*lockingTarget) ContentOnly(path string) bool { return sqlFileOnly(path) }
