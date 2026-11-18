package cli

// rulesfile.go — `check --rules-file PATH` (TakeoffQS#17436): run exactly the
// rule ids a file lists. A selector outside the engine decides which rules a
// change can affect; the engine's half is to run that list and nothing else,
// and to refuse rather than shrink it. Split out of cli.go, which sits near
// the 750-line vendor cap.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
)

const rulesFileUsage = "run only the rule ids this file lists, one per line (blank lines and # comments ignored). " +
	"Every listed id must be a rule the selected lane runs, or the run is refused; --shard partitions the listed set. " +
	"The JSON report's scan.selection says the run judged a selection, not the whole lane"

// listedRuleIDs reads the --rules-file the caller supplied, in file order with
// duplicates dropped. supplied is false when the flag was absent. Every way the
// file cannot name a rule to run — an empty flag value, an unreadable file, a
// file that lists nothing — is printed and returned as !ok: each would
// otherwise become a run over some other set than the one asked for.
func listedRuleIDs(fs *flag.FlagSet, path string, stderr io.Writer) (ids []string, supplied, ok bool) {
	fs.Visit(func(f *flag.Flag) { supplied = supplied || f.Name == "rules-file" })
	if !supplied {
		return nil, false, true
	}
	if path == "" {
		fmt.Fprintln(stderr, "formwork: --rules-file was given an empty value — refusing to fall back to the whole lane; pass the file of rule ids, or drop the flag")
		return nil, true, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "formwork: --rules-file: %v\n", err)
		return nil, true, false
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		id := strings.TrimSpace(line)
		if id == "" || strings.HasPrefix(id, "#") || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fmt.Fprintf(stderr, "formwork: --rules-file %s lists no rule id, so this run would check nothing; list the rules to run, or drop the flag to run the whole lane\n", path)
		return nil, true, false
	}
	return ids, true, true
}

// selectListed narrows rls, the lane's selection in its own order, to the
// listed ids. An id the lane does not run is an error naming every such id: the
// file narrows the lane and never widens it, and dropping an id it could not
// find would shrink the run without a word.
func selectListed(rls []*config.Rule, ids []string) ([]*config.Rule, error) {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []*config.Rule
	for _, r := range rls {
		if want[r.ID] {
			out = append(out, r)
			delete(want, r.ID)
		}
	}
	if len(want) > 0 {
		var missing []string
		for _, id := range ids {
			if want[id] {
				missing = append(missing, id)
			}
		}
		return nil, fmt.Errorf("--rules-file lists %d id(s) the selected lane does not run: %s — refusing to run a smaller set than the file names", len(missing), strings.Join(missing, ", "))
	}
	return out, nil
}
