package cli_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// Every -format json answer is one object, its list a named member: a bare
// top-level array is a shape no declared message decodes without the
// consumer framing it in code.
func TestEveryListJSONAnswerIsAnObject(t *testing.T) {
	toy := filepath.Join("testdata", "toyrepo")
	for _, tc := range []struct {
		argv []string
		key  string
	}{
		{[]string{"list", "-format", "json", "types"}, "types"},
		{[]string{"list", "-format", "json", "preprocessors"}, "preprocessors"},
		{[]string{"list", "-C", toy, "-format", "json", "rules"}, "rules"},
		{[]string{"rules-for", "-C", toy, "-format", "json", "README.md"}, "paths"},
	} {
		code, out, errOut := runCLI(t, tc.argv...)
		if code != 0 {
			t.Fatalf("%v: exit %d\n%s%s", tc.argv, code, out, errOut)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("%v answered something other than an object: %v\n%s", tc.argv, err, out)
		}
		var list []json.RawMessage
		if err := json.Unmarshal(doc[tc.key], &list); err != nil || len(list) == 0 {
			t.Fatalf("%v: member %q is not a non-empty list (%v):\n%s", tc.argv, tc.key, err, out)
		}
	}
}
