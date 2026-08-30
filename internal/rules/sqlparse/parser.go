// Package sqlparse implements the parse-tree-backed SQL rule types (sql/parses,
// sql/locking-select-order). It wraps github.com/wasilibs/go-pgquery — the real
// PostgreSQL 17 grammar compiled to WASM and run on wazero (pure Go, no cgo) —
// and hands WHOLE SQL text to the parser, which does its own statement
// splitting. The rules never pre-split on ';'. Nothing outside this package
// imports go-pgquery.
package sqlparse

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// defaultParseConcurrency bounds how many callers may be inside the WASM parser
// at once (#83).
//
// go-pgquery serves concurrent callers from an internal pool of WASM instances,
// and EACH INSTANCE COSTS ROUGHLY 250 MB RESIDENT. The pool grows to the peak
// concurrency it ever sees, never shrinks, and the cost is invisible to
// HeapAlloc because WASM linear memory is not live Go objects. Measured, 100
// parses of a single 4-line SELECT so corpus size is not a variable:
//
//	callers   HeapAlloc   maxRSS
//	      1        8 MB    292 MB
//	      4       28 MB  1,110 MB
//	     16      110 MB  4,020 MB
//
// It scales with CORE COUNT, not corpus size — and sql/* rules are CostFast, so
// without a bound they run at full worker width. A 16-core CI runner pays 4 GB
// to parse a handful of SELECTs, and an adopter meets that on their own hardware
// before they have any reason to suspect the SQL rules.
//
// Four is chosen against the table above: ~1 GB peak, which fits a runner that
// also has to hold a Go toolchain, and parsing is not the bottleneck the issue's
// measurements were looking for. Deliberately NOT GOMAXPROCS — the whole defect
// is that this scales with cores when it should not.
const defaultParseConcurrency = 4

// parseConcurrencyEnv lets an operator with memory to spare raise the bound, or
// one on a small runner lower it. Named rather than inferred: guessing from
// available memory is fragile in a container, where the number the runtime sees
// is often the host's.
const parseConcurrencyEnv = "FORMWORK_SQL_PARSER_CONCURRENCY"

// parseSem admits at most N callers to the parser. Buffered channel rather than
// a semaphore type so the bound is readable as cap(parseSem) in a test.
var parseSem = make(chan struct{}, parseConcurrency())

func parseConcurrency() int {
	if v := os.Getenv(parseConcurrencyEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		// A malformed value falls back to the default rather than refusing: this
		// is a performance knob, and failing a whole run over a typo in it would
		// trade a memory problem for an availability one.
		//
		// NOT reported anywhere. An operator who raises this gets no disclosure
		// that they did, which is a gap — the escape-hatch census is where it
		// would belong, and sqlparse cannot reach internal/meta without an import
		// cycle. Said here rather than left for a reader to assume the census
		// covers it, because it does not.
	}
	return defaultParseConcurrency
}

// parsePeak is the highest concurrency ever observed inside the parser. Read by
// the bound's test, which cannot otherwise tell a working semaphore from a
// serialised workload that never reached the limit.
var parseInFlight, parsePeak atomic.Int64

// parse parses whole SQL text into its statement list. A syntax error is
// returned as err (pg_query returns no partial result on error).
//
// Safe for concurrent use — go-pgquery serves each call from an internal
// mutex-guarded pool — but deliberately BOUNDED before entry, because that pool
// sizes itself to peak concurrency and never gives the memory back.
func parse(sql string) (*pg.ParseResult, error) {
	parseSem <- struct{}{}
	n := parseInFlight.Add(1)
	for {
		peak := parsePeak.Load()
		if n <= peak || parsePeak.CompareAndSwap(peak, n) {
			break
		}
	}
	defer func() {
		parseInFlight.Add(-1)
		<-parseSem
	}()
	return pgquery.Parse(sql)
}

// SelfBounded routes this rule type through the BOUNDED half of the engine's
// phase-1 partition (internal/engine/engine.go — the `selfBounded` interface
// and the boundRls/wideRls split in Run).
//
// Declared here rather than beside each type because parseSem above is what
// the declaration is about: a reader changing the semaphore needs its three
// consumers in front of them, and a reader of a rule file needs one place that
// explains all three.
//
// Every rule type this package registers enters parse() SYNCHRONOUSLY from
// CheckFile — *parses through statements(), *lockingOrder and *lockingTarget
// through lockingStatements(), all three via parseChunk (source.go) — so the
// `parseSem <- struct{}{}` above executes on the engine's phase-1 worker
// goroutine. A worker parked on that send still holds its file and takes
// nothing else off the queue, so in one flat pool the bound stops throttling
// the parser and starts throttling every OTHER rule in the phase. Splitting
// the pool is what keeps this bound the parser's alone (#83 acceptance
// criterion 2, #315).
//
// Cost is NOT the discriminator and could not be. *lockingTarget declares
// Cost() CostFast explicitly and the other two default to it, so a partition
// keyed on CostHeavy would route all three into the full-width half — while
// sweeping in the heavy rule types, whose resource is a subprocess already
// governed by phase 2's own pools and the machine-wide gate, not this
// semaphore. internal/engine/phase1_sqlparse_wiring_test.go pins both
// directions against the real registry.
func (*parses) SelfBounded() bool { return true }

// SelfBounded — see (*parses).SelfBounded above. lockingStatements reaches
// parse() through parseChunk on both the .sql and the .go path.
func (*lockingOrder) SelfBounded() bool { return true }

// SelfBounded — see (*parses).SelfBounded above. Shares lockingStatements with
// *lockingOrder, so it takes the same admission on the same paths.
func (*lockingTarget) SelfBounded() bool { return true }

// lineAt returns the 1-based line of byte offset off within content.
func lineAt(content string, off int) int {
	if off < 0 || off > len(content) {
		off = 0
	}
	return 1 + strings.Count(content[:off], "\n")
}
