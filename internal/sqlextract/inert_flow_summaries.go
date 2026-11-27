package sqlextract

import (
	"go/ast"
	"strconv"
	"strings"
)

// inert_flow_summaries.go — A HELPER THAT ONLY RETURNS ITS TEXT IS NOT A SHARED NODE.
//
// A function has one node for its parameters and one for its return, so text given to
// it at one call seems to come back at every other. A helper called with a statement in
// one place and a column type in another then carries the statement into the type.
//
// So a function whose parameters reach nothing outside it but its own return is
// summarised: at a call, an argument goes on as the call's result, or ends there when
// its parameter never reaches the return. Nothing else it could do with the text is
// left out, because anything else is an edge out of the function, and any such edge
// leaves it unsummarised. A function only calling summarised functions qualifies in a
// later round, so the rounds are bounded and an unfinished one only reads less.
//
// Not summarised: a method, which keeps its edges at every call; a
// function with a builder or writer parameter, whose text goes out by that parameter;
// and one with an unnamed parameter, which says nothing about what it would do.
const maxSummaryRounds = 5

// funcRange is where a package-level function sits in its file.
type funcRange struct {
	lo, hi int
	key    string
}

// noteFuncRange records fd's span under its node.
func (ff *flowFile) noteFuncRange(fd *ast.FuncDecl) {
	ff.g.funcRanges[ff.path] = append(ff.g.funcRanges[ff.path], funcRange{
		lo: ff.fset.Position(fd.Pos()).Offset, hi: ff.fset.Position(fd.End()).Offset, key: ff.funcKeyOf(fd)})
}

// summarised returns the graph's summaries: for each function it can summarise, whether
// each parameter reaches the return.
func (g *flowGraph) summarised() map[string][]bool {
	out := map[string][]bool{}
	for fn, info := range g.funcs {
		if strings.HasPrefix(fn, "method.") || strings.HasPrefix(fn, "closure ") || len(info.params) == 0 {
			continue
		}
		if reaches, ok := g.summary(fn, info); ok {
			out[fn] = reaches
		}
	}
	return out
}

func (g *flowGraph) summary(fn string, info *flowFunc) ([]bool, bool) {
	reaches := make([]bool, len(info.params))
	for i := range info.params {
		if info.plainParams[i] {
			continue
		}
		if info.builderParams[i] || info.writerParams[i] || info.params[i] == "" {
			return nil, false
		}
		seen := map[string]bool{}
		queue := []string{paramKey(fn, i)}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			for _, to := range g.edges[n] {
				if to == retKey(fn) {
					reaches[i] = true
					continue
				}
				if seen[to] {
					continue
				}
				seen[to] = true
				if !g.insideFunc(to, fn) {
					return nil, false
				}
				queue = append(queue, to)
			}
		}
	}
	return reaches, true
}

// insideFunc reports whether node is a local, a composition or a closure in fn's span,
// or fn's own parameter.
func (g *flowGraph) insideFunc(node, fn string) bool {
	if strings.HasPrefix(node, "param "+fn+" ") {
		return true
	}
	path, offset, ok := nodePosition(node)
	if !ok {
		return false
	}
	for _, r := range g.funcRanges[path] {
		if r.key == fn && offset >= r.lo && offset < r.hi {
			return true
		}
	}
	return false
}

// nodePosition reads the file and offset out of a node that sits at a place.
func nodePosition(node string) (string, int, bool) {
	for _, prefix := range []string{"local ", "site ", "param closure ", "ret closure "} {
		if !strings.HasPrefix(node, prefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(node, prefix), ":")
		if len(parts) < 2 {
			return "", 0, false
		}
		end := parts[1]
		if i := strings.IndexAny(end, "- "); i >= 0 {
			end = end[:i]
		}
		offset, err := strconv.Atoi(end)
		return parts[0], offset, err == nil
	}
	return "", 0, false
}
