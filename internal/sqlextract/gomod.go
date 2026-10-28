package sqlextract

import (
	"path"
	"strconv"
	"strings"
)

// gomod.go — where an import path lives in the walk, for consts.go.
//
// The go toolchain's answer, cut to what the walk can hold: the importing
// file's module (the nearest go.mod above it) owns the paths under its module
// path, and a replace directive pointing at a directory moves a module path
// there. Anything else is a module outside the tree — the standard library, a
// dependency in the module cache — whose constants the extractor does not read.

// goMod is the part of a go.mod the import resolution reads.
type goMod struct {
	dir, module string
	// replace maps a replaced module path to the repo-relative directory a
	// replace directive points it at.
	replace map[string]string
}

// modFor returns the module whose go.mod is nearest above dir, or nil.
func (e *constEnv) modFor(dir string) *goMod {
	for d := dir; ; d = path.Dir(d) {
		e.mu.Lock()
		m, seen := e.mods[d]
		e.mu.Unlock()
		if seen {
			if m != nil {
				return m
			}
		} else {
			m = e.readMod(d)
			e.mu.Lock()
			e.mods[d] = m
			e.mu.Unlock()
			if m != nil {
				return m
			}
		}
		if d == "." || d == "/" {
			return nil
		}
	}
}

// readMod parses dir/go.mod, or returns nil when the walk holds none.
func (e *constEnv) readMod(dir string) *goMod {
	f := e.tree.File(path.Join(dir, "go.mod"))
	if f == nil {
		return nil
	}
	src, err := f.Content()
	if err != nil {
		return nil
	}
	m := &goMod{dir: dir, replace: map[string]string{}}
	inReplace := false
	for _, line := range strings.Split(string(src), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "module" && len(fields) == 2:
			m.module = unquoteModPath(fields[1])
			continue
		case fields[0] == "replace" && len(fields) == 2 && fields[1] == "(":
			inReplace = true
			continue
		case inReplace && fields[0] == ")":
			inReplace = false
			continue
		case fields[0] == "replace":
			fields = fields[1:]
		case !inReplace:
			continue
		}
		// old [version] => new [version]
		arrow := -1
		for i, f := range fields {
			if f == "=>" {
				arrow = i
			}
		}
		if arrow < 1 || arrow+1 >= len(fields) {
			continue
		}
		target := unquoteModPath(fields[arrow+1])
		if !strings.HasPrefix(target, "./") && !strings.HasPrefix(target, "../") && target != "." && target != ".." {
			continue
		}
		to := path.Clean(path.Join(dir, target))
		if to == ".." || strings.HasPrefix(to, "../") {
			continue // outside the walk
		}
		m.replace[unquoteModPath(fields[0])] = to
	}
	if m.module == "" {
		return nil
	}
	return m
}

func unquoteModPath(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

// importDir returns the repo-relative directory the import path ip names for a
// file in fromDir, or "" when it is not in the walk.
func (e *constEnv) importDir(fromDir, ip string) string {
	m := e.modFor(fromDir)
	if m == nil {
		return ""
	}
	var dir string
	switch {
	case ip == m.module:
		dir = m.dir
	case strings.HasPrefix(ip, m.module+"/"):
		dir = path.Join(m.dir, ip[len(m.module)+1:])
	default:
		best := ""
		for old := range m.replace {
			if (ip == old || strings.HasPrefix(ip, old+"/")) && len(old) > len(best) {
				best = old
			}
		}
		if best == "" {
			return ""
		}
		dir = path.Join(m.replace[best], strings.TrimPrefix(ip[len(best):], "/"))
	}
	if len(e.tree.Dir(dir)) == 0 {
		return ""
	}
	return dir
}
