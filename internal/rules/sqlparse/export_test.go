package sqlparse

// Test-only accessors for the parser bound (#83). The peak matters as much as
// the cap: a test that only asserts cap(parseSem) proves the channel was made
// with a size, not that anything ever waited on it.
func ParseBound() int          { return cap(parseSem) }
func ParsePeak() int64         { return parsePeak.Load() }
func ResetParsePeak()          { parsePeak.Store(0) }
func ParseConcurrencyFor() int { return parseConcurrency() }
