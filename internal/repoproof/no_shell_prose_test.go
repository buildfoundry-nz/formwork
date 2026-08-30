// no_shell_prose_test.go — the enumerated exception must be able to count
// what it excuses, in the prose as well as in the map.
//
// no_shell_test.go excuses a set of tracked husky shims BY NAME (huskyShims)
// and then describes that set in English: how many paths it holds, which hook
// is the first one NOT excused, and how many of the pinned paths the index
// carries as executable. #264 r2's whole argument is that an exception which
// cannot count what it is excusing is the same defect as a predicate that
// cannot read the file it is judging — the reader is told a number and has no
// way to find out it is still true.
//
// It stopped being true, exactly as filed. 6a05c814 grew the pin from twelve
// paths to twenty, corrected the three counts in the lower half of the file,
// and left the header saying twelve named paths, twelve husky hooks, a
// thirteenth hook, and four of twelve carrying mode 0755 — when twelve of
// twenty do. Nothing could see it: `grep -rn huskyShims --include='*.go'`
// reaches one file, and no test read the prose.
//
// So the counts are read out of the map and out of the git index here rather
// than spelled a second time in English and trusted. The mechanism is the one
// hooks_e2e_test.go already uses for the Makefile's coverage claim
// (makeCommentBlock / corpusTheProofAdvertises): unwrap the comment, find the
// claim, compare it with the thing it describes.
//
// EVERY CLAIM BELOW IS REQUIRED TO APPEAR. A sentence that has been deleted,
// or reworded past the pattern that finds it, fails here — because a pin a
// deletion satisfies pins nothing, and "drop the sentence" is the cheapest way
// to make a prose assertion green while making the file less honest.
//
// This test makes the coupling between huskyShims and the fixture tree LOUDER,
// not smaller: the census still names twenty fixture paths exactly, so moving
// a fixture directory still moves the census — and now it moves the prose too,
// which is the point.
package repoproof_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// censusSource is the file whose prose describes huskyShims.
const censusSource = "internal/repoproof/no_shell_test.go"

// commentProse returns every run of `//` comment lines in path, each run
// unwrapped onto ONE line so an assertion is not hostage to where the prose
// happens to wrap. Runs are kept apart by newlines so no pattern can match
// across two unrelated blocks.
//
// Fail-closed on an unreadable or comment-free file: counts compared against
// nothing compare equal to nothing.
func commentProse(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s, so the prose describing the enumerated exception "+
			"cannot be compared with the exception: %v", censusSource, err)
	}
	var runs, run []string
	flush := func() {
		if len(run) > 0 {
			runs = append(runs, strings.Join(strings.Fields(strings.Join(run, " ")), " "))
			run = nil
		}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "//"); ok {
			run = append(run, rest)
			continue
		}
		flush()
	}
	flush()
	if len(runs) == 0 {
		t.Fatalf("%s carries no comment prose at all — every count below would be "+
			"checked against an empty string and pass", censusSource)
	}
	return strings.Join(runs, "\n")
}

// The cardinals and ordinals English spells irregularly, so a measured integer
// can be compared with the word the prose writes. Only the range a fixture
// census plausibly reaches is spelled; beyond it this fails rather than
// approximates, because a pin that guesses at the word it wants is not a pin.
var (
	cardinalOnes = []string{
		"zero", "one", "two", "three", "four", "five", "six", "seven", "eight",
		"nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
		"sixteen", "seventeen", "eighteen", "nineteen",
	}
	cardinalTens = []string{
		"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy",
		"eighty", "ninety",
	}
	irregularOrdinals = map[string]string{
		"one": "first", "two": "second", "three": "third", "five": "fifth",
		"eight": "eighth", "nine": "ninth", "twelve": "twelfth",
	}
)

func spellCardinal(t *testing.T, n int) string {
	t.Helper()
	switch {
	case n < 0 || n > 99:
		t.Fatalf("%d is outside the range this test can spell, so the prose it "+
			"checks cannot be stated — widen the speller, never drop the claim", n)
		return ""
	case n < len(cardinalOnes):
		return cardinalOnes[n]
	case n%10 == 0:
		return cardinalTens[n/10]
	default:
		return cardinalTens[n/10] + "-" + cardinalOnes[n%10]
	}
}

func spellOrdinal(t *testing.T, n int) string {
	t.Helper()
	suffixed := func(word string) string {
		if o, ok := irregularOrdinals[word]; ok {
			return o
		}
		if rest, ok := strings.CutSuffix(word, "y"); ok {
			return rest + "ieth"
		}
		return word + "th"
	}
	cardinal := spellCardinal(t, n)
	if tens, ones, hyphenated := strings.Cut(cardinal, "-"); hyphenated {
		return tens + "-" + suffixed(ones)
	}
	return suffixed(cardinal)
}

// pinnedModes asks the index what mode it records for every pinned husky shim.
//
// A pinned path the index does not carry is FATAL rather than skipped: the
// mode sentence is a statement about the pinned set, and a count taken over
// part of that set is not a count of it. The lookup is scrubbed for the same
// reason every other lookup in this package is (#264 r1) — a mode read out of
// somebody else's index would ratify whatever the prose already said.
func pinnedModes(t *testing.T, root string) (total, executable int) {
	t.Helper()
	needBinary(t, "git")
	pins := slices.Sorted(maps.Keys(huskyShims))
	out, err := gitScrubbed(root, append([]string{"ls-files", "-s", "-z", "--"}, pins...)...).Output()
	if err != nil {
		t.Fatalf("cannot ask git for the mode of the pinned husky shims: %v", err)
	}
	mode := map[string]string{}
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			t.Fatalf("git ls-files -s printed a record with no path: %q", record)
		}
		bits, _, ok := strings.Cut(meta, " ")
		if !ok {
			t.Fatalf("git ls-files -s printed a record with no mode: %q", record)
		}
		mode[path] = bits
	}
	for _, p := range pins {
		m, ok := mode[p]
		if !ok {
			t.Fatalf("%s is pinned as an excused husky shim but the index does not "+
				"carry it, so the mode claim in %s cannot be measured over the set it "+
				"describes", p, censusSource)
		}
		if m == "100755" {
			executable++
		}
	}
	return len(pins), executable
}

// prosePin is one numeric claim the census file makes about its own exception:
// what the sentence says, the pattern that finds the number in it, and the
// word the measured tree puts there.
type prosePin struct {
	claim string
	find  *regexp.Regexp
	want  string
}

func checkProsePins(t *testing.T, prose string, pins []prosePin) {
	t.Helper()
	for _, p := range pins {
		found := p.find.FindAllStringSubmatch(prose, -1)
		if len(found) == 0 {
			t.Errorf("%s no longer states %s. A count this test cannot find is a count "+
				"nobody has to keep true, so a deleted or reworded sentence fails here "+
				"rather than passing quietly. Pattern: %s", censusSource, p.claim, p.find)
			continue
		}
		for _, m := range found {
			if got := strings.ToLower(m[1]); got != p.want {
				t.Errorf("%s states %s as %q; the tree says %q. The exception is "+
					"enumerated precisely so its prose can be checked against it — prose "+
					"that has drifted from the map is the defect #264 r2 filed.",
					censusSource, p.claim, got, p.want)
			}
		}
	}
}

// #264 r2, the header half — the prose that describes huskyShims must agree
// with huskyShims and with the index.
//
// Two independent claims, so two subtests: the SIZE of the enumerated set (and
// therefore which hook is the first one not excused), and how many of the
// pinned shims the index records as executable. They fail separately because
// they can drift separately — 6a05c814 got the size right in three places and
// wrong in three others, and never touched the mode sentence at all.
func TestCensusProseCountsTheExceptionItDescribes(t *testing.T) {
	root := repoRoot(t)
	prose := commentProse(t, filepath.Join(root, filepath.FromSlash(censusSource)))
	total, executable := pinnedModes(t, root)

	t.Run("the prose counts the paths the pin enumerates", func(t *testing.T) {
		size, next := spellCardinal(t, total), spellOrdinal(t, total+1)
		checkProsePins(t, prose, []prosePin{
			{"how many named paths the exception is",
				regexp.MustCompile(`it is ([a-z-]+) named paths`), size},
			{"how many husky hooks live under the corpus fixture tree",
				regexp.MustCompile("([A-Za-z-]+) `#!/usr/bin/env sh` husky hooks live"), size},
			{"how long the ENUMERATED list is",
				regexp.MustCompile(`ENUMERATED list of ([a-z-]+) paths`), size},
			{"how many files huskyShims holds",
				regexp.MustCompile(`These ([a-z-]+) files are INPUT BYTES`), size},
			{"how many husky hooks the path shape covers today",
				regexp.MustCompile(`covers ([a-z-]+) husky hooks today`), size},
			{"how many corpus hooks the reader is told are excused",
				regexp.MustCompile(`reader is told ([a-z-]+) corpus hooks are excused`), size},
			{"which hook is reported rather than excused",
				regexp.MustCompile(`a ([a-z-]+) hook is reported like any other`), next},
			{"which hook the path shape would swallow next",
				regexp.MustCompile(`it would cover a ([a-z-]+), and a hundredth`), next},
		})
	})

	t.Run("the mode sentence counts what the index records as executable", func(t *testing.T) {
		checkProsePins(t, prose, []prosePin{
			{"how many pinned shims carry mode 0755",
				regexp.MustCompile(`([a-z-]+) of the [a-z-]+ carry mode 0755`),
				spellCardinal(t, executable)},
			{"how many pinned shims that count is out of",
				regexp.MustCompile(`[a-z-]+ of the ([a-z-]+) carry mode 0755`),
				spellCardinal(t, total)},
		})
	})
}
