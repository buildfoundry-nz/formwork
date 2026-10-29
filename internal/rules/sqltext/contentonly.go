package sqltext

import "github.com/buildfoundry-nz/formwork/internal/sqlextract"

func (*predicate) ContentOnly(path string) bool { return sqlextract.FileKind(path) == "sql" }
