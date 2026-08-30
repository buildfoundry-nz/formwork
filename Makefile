.PHONY: all help build test check selftest lint corpus-disclosure-proof quickstart-proof hooks-e2e-proof sync-proof gate gate-proof vet fmt verify clean sync sync-status

PROJECTS_DIR ?= projects
REPOS_FILE   ?= repos.txt

# GO_RUN runs a cmd/ binary preserving ITS exit code. `go run` collapses any
# non-zero child exit to its own exit 1 ("exit status N"), which folds an
# engine/config error (2) into findings (1) through the make surface — the
# repo's exit-code contract (0 pass, 1 violations, 2 error) holds only for the
# binaries unless the target builds to a throwaway binary and runs that.
# Targets whose 1-vs-2 distinction is load-bearing use this; the selftest/lint
# corpus loops keep plain `go run` because they deliberately fold every corpus
# verdict into one exit 1, and the child's own code still prints on the
# "exit status N" line.
GO_RUN = bin=$$(mktemp "$${TMPDIR:-/tmp}/formwork-gorun.XXXXXX"); go build -o "$$bin" $(1) && "$$bin" $(2); rc=$$?; rm -f "$$bin"; exit $$rc

# proof runs a -run-filtered test target FAIL-CLOSED on an empty selection.
# $(1) is the package, $(2) the -run regex.
#
# `go test <pkg> -run <regex>` exits 0 with "[no tests to run]" when the regex
# matches nothing. Every proof target in this file names the tests that carry
# the property it advertises, so that default turns "the tests are gone" into
# a green `ok` line — measured on relay-proof (#278): with relay_test.go moved
# away AND the one-home property broken in internal/buildloop/record.go, the
# target printed `ok ... [no tests to run]` and exited 0 twice running.
#
# The selection is therefore COUNTED before it is run. `go test -list` answers
# the same regex without executing anything, so the count costs one cached
# build and nothing else. A list that cannot be produced (the package does not
# compile) is exit 2 as well — a proof that cannot enumerate its own subject
# has not proved anything either.
#
# Whack-a-mole was the alternative and was rejected: nine of thirteen targets
# had the defect, and the tenth acquires it the moment someone writes a new
# one. internal/repoproof's TestNoRecipeSpellsARunFilteredGoTestItself refuses
# any recipe that spells `go test ... -run` without coming through here, and
# TestEveryProofCallSelectsAtLeastOneTest counts every selection in the file.
define proof
sel=$$(go test $(1) -list '$(2)' 2>&1) || { \
	echo "$(1): cannot list tests matching '$(2)' — a proof that cannot enumerate its subject has proved nothing" >&2; \
	echo "$$sel" >&2; exit 2; }; \
n=$$(printf '%s\n' "$$sel" | grep -c '^Test'); \
if [ "$$n" -eq 0 ]; then \
	echo "$(1): -run '$(2)' matches NO test — go test would exit 0 over an empty selection, so this target would report a pass over a property nothing checks" >&2; \
	exit 2; \
fi; \
go test $(1) -run '$(2)' -count=1
endef

# `gate` promises its verdict is the LAST line of stdout, so `make gate | tail -1`
# cannot be misread. GNU make >= 4 breaks that promise from OUTSIDE the recipe:
# whenever it runs as a sub-make (MAKELEVEL set in the environment) it turns -w on
# by default and prints `make[1]: Leaving directory ...` after the recipe returns.
# The verdict then sits second-to-last and a tail -1 reader sees a directory path.
# No recipe can suppress a line its own parent process prints on exit.
#
# What this assignment buys, exactly — measured, not assumed, because the first
# cut of this comment over-generalised from a single container and CI caught it:
#
#   make 3.81 (macOS)  prints no directory lines for a sub-make at all; moot
#   make 4.3 (Ubuntu)  IGNORES a makefile-level MAKEFLAGS for its own process,
#                      but still propagates it to every sub-make
#   make 4.4.1         honours it for its own process too (behaviour changed in
#                      4.4; the -w decision is re-read rather than fixed at
#                      startup)
#
# So on 4.3 this line does not help the make that parses it — it helps every
# make BELOW it. That covers the invocations this repo actually makes: `make
# gate-proof`, and any `$(MAKE) gate` from a target here, all reach `gate` as a
# sub-make that inherits the flag. Dropping the line turned 6 gate-proof vectors
# red on ubuntu-latest; restoring it left exactly one.
#
# The residual hole, stated rather than papered over: a FOREIGN wrapper Makefile
# on make < 4.4 that invokes `make -C <repo> gate` with -w on and without the
# flag in MAKEFLAGS gets make's `Leaving directory` after the verdict. Unfixable
# from inside this Makefile on that version; internal/repoproof's TestGate probes
# for the capability and asserts the strongest contract the running make can support,
# rather than reporting a green it did not earn.
MAKEFLAGS += --no-print-directory

# First target, so a bare `make` still runs the full verification.
all: verify

##@ Everyday

## help: list every target in this Makefile
# Self-documenting: the listing is generated from the `## name: description`
# lines above each target and the `##@ Section` headers, so a target added
# without a doc comment is invisible here — which is the nudge to write one.
# Plain `#` comments are implementation notes and stay out of the listing.
help:
	@awk '/^##@ /{printf "\n%s\n", substr($$0,5)} \
	      /^## [a-zA-Z0-9_-]+:/{l=substr($$0,4); i=index(l,":"); \
	                         printf "  %-13s %s\n", substr(l,1,i-1), substr(l,i+2)} \
	      END{printf "\n"}' $(MAKEFILE_LIST)

## verify: everything CI would run — the whole gate; read the verify target below for the current prerequisite list rather than trusting a copy here
# PKG=/RUN= are inner-loop knobs for `make test` alone; verify must ALWAYS run the
# full suite. A command-line assignment (`make verify PKG=./internal/cli`) is global
# to the whole invocation and would otherwise reach the `test` prerequisite, scoping
# the gate down to one package while vet/fmt/check/selftest/lint still pass — a green
# exit 0 that never ran most of the race suite. Target-scoped `override` forces both
# empty for verify and every prerequisite it builds, and `override` beats a
# command-line assignment, so the narrowing knobs are unreachable from the gate.
verify: override PKG :=
verify: override RUN :=
verify: test vet fmt check selftest lint corpus-disclosure-proof quickstart-proof hooks-e2e-proof sync-proof gate-proof

# The inner command `gate` runs, and the log it writes. Both are overridable, and
# GATE_CMD exists FOR internal/repoproof's TestGate: the proof has to drive the
# gate
# against a suite that deliberately fails, and running the real `make verify`
# twice per proof would put minutes into `make verify` itself.
#
# GATE_CMD MUST NEVER BE USED TO NARROW A REAL GATE RUN. This repo has the exact
# precedent — see the `verify: override PKG :=` block above, which exists because
# a command-line assignment silently scoped the gate to one package while every
# other prerequisite still passed, producing a green exit 0 over most of the
# suite unrun. The remedy there was to make the knob unreachable; here the knob
# must stay reachable for the proof, so `gate` instead ANNOUNCES an overridden
# inner command on the line immediately above its verdict. A narrowed run cannot
# be pasted into a transcript as evidence of a full one.
#
# The default log lives in $TMPDIR, deliberately outside the repo: an untracked
# file in the worktree would be visible to the `check` and `selftest` runs
# happening inside this very invocation.
#
# The overridden/not-overridden question is answered at PARSE time, by exact
# string comparison, and never by quoting GATE_CMD into a shell test — an inner
# command carrying quotes of its own would otherwise break the very announcement
# that keeps it honest. The NOTE deliberately does not echo the value back for
# the same reason.
GATE_DEFAULT_CMD := $(MAKE) verify
GATE_CMD ?= $(GATE_DEFAULT_CMD)
GATE_LOG ?=
ifeq ($(GATE_CMD),$(GATE_DEFAULT_CMD))
GATE_OVERRIDDEN :=
else
GATE_OVERRIDDEN := 1
endif

## gate: run verify and end in an unmissable GATE: PASS / GATE: FAIL verdict
# `make verify 2>&1 | tail -40` reports TAIL's exit status, not make's. That is
# how four consecutive "verify is green" claims got made in one session while
# the failures sat in captured output nobody had the status of — CI caught them,
# the local gate did not. A repo that gates hooks, sync, the corpora and its own
# 0/1/2 exit contract mechanically should not have a misreadable gate.
#
# The fix is structural: the capture happens INSIDE the recipe, so there is no
# pipeline left for the operator to lose the status in. `gate` writes the suite's
# output to a log, tests make's OWN status, re-emits the log, and then exits with
# that status — with the verdict as the last line of STDOUT. So `make gate |
# tail -1` still shows PASS/FAIL, and under a pipefail shell (CI's default) the
# nonzero status survives the pipe as well. Without pipefail the pipeline still
# reports tail's status; nothing a target can do about that, which is exactly why
# the verdict line exists.
#
# Two boundary facts the verdict is shaped around, both pinned by the proof:
# make appends its own `*** [gate] Error N` on STDERR after the recipe returns,
# unsuppressable without also discarding the status — so a reader who merges the
# streams gets the verdict immediately above make's error line and both say
# failure. And make flattens every recipe failure to exit 2, so the inner suite's
# own status (this repo's 1-vs-2 contract) is carried in the verdict TEXT; what
# survives the process boundary is nonzero-vs-zero.
#
# On failure the failing target (make's `***` line) and the tail of the log are
# re-surfaced adjacent to the verdict, so a short `tail` is actionable rather
# than a prompt to go hunting; the full log is kept and its path printed.
# internal/repoproof's TestGate pins both directions.
gate:
	@log="$(GATE_LOG)"; created=; \
	if [ -z "$$log" ]; then \
		log=$$(mktemp "$${TMPDIR:-/tmp}/formwork-gate.XXXXXX") || \
			{ echo "GATE: FAIL (cannot create a log file)"; exit 2; }; \
		created=1; \
	fi; \
	$(GATE_CMD) >"$$log" 2>&1; rc=$$?; \
	cat "$$log"; \
	if [ "$$rc" -ne 0 ]; then \
		echo "GATE: ---- failure context ----"; \
		grep '\*\*\*' "$$log" | tail -3 | sed 's/^/GATE: /'; \
		tail -20 "$$log" | sed 's/^/GATE: | /'; \
		echo "GATE: full log: $$log"; \
	fi; \
	if [ -n "$(GATE_OVERRIDDEN)" ]; then \
		echo "GATE: NOTE — GATE_CMD was OVERRIDDEN; this is NOT a full gate run"; \
	fi; \
	if [ "$$rc" -eq 0 ]; then \
		if [ -n "$$created" ]; then rm -f "$$log"; fi; \
		echo "GATE: PASS"; \
	else \
		echo "GATE: FAIL (exit $$rc)"; \
	fi; \
	exit $$rc

## build: compile the formwork binary into ./formwork
build:
	go build ./cmd/formwork

##@ Individual checks (verify runs all of these)

# Every Go package in THIS module, excluding the validating-target clones under
# projects/. Same reasoning as the fmt target below, and the same failure: `./...`
# is a filesystem walk with no notion of .gitignore, so once `make sync` has
# materialised the clone, its scripts/dev/*.go and scripts/lib/*.go — dozens of
# standalone programs, each with its own `func main` in one directory — are swept
# into this module, and `go test` / `go vet` fail on the redeclarations before
# they reach a line of formwork's own code. That made `make verify` unrunnable for
# anyone who had followed the documented workflow. Filtering `go list` output keeps
# foreign source out by construction rather than by an exclusion list that rots.
#
# The filter is anchored to the module path — `<module>/$(PROJECTS_DIR)/`, not a
# bare `/$(PROJECTS_DIR)/` substring — so it can only ever drop the clones, never
# a real formwork package whose import path happens to contain "projects". That
# anchor depends on `go list -m`, so THAT is the lookup checked fail-closed: an
# empty module path means we cannot tell clones from real code and must stop.
#
# `go list ./...`'s own exit status is deliberately NOT treated as fatal. A
# pinned validating target can carry Go that does not resolve against this
# module's go.mod; `go list ./...` then exits non-zero but still prints every
# package it COULD load on stdout, and the unresolved ones are under projects/ —
# exactly what the filter drops. Aborting on that exit (an earlier version did)
# re-broke `make verify` after `make sync`, the very failure this define exists
# to prevent. A load error in formwork's OWN code is not silently swallowed: go
# prints it to stderr here, and `go test`/`go vet` surface it when they run. The
# real fail-closed is the empty-result check: if filtering leaves nothing, the
# environment is broken in a way that must not read as green.
define go_pkgs
module=$$(go list -m); \
if [ -z "$$module" ]; then echo "cannot determine module path (go list -m)" >&2; exit 1; fi; \
pkgs=$$(go list ./... | grep -v "^$$module/$(PROJECTS_DIR)/"); \
if [ -z "$$pkgs" ]; then echo "no packages to build after filtering $(PROJECTS_DIR)/" >&2; exit 1; fi
endef

## test: run the full suite with the race detector (PKG=./pkg RUN=Pattern to narrow)
# PKG and RUN are inner-loop knobs, set on the command line, that narrow WITHOUT
# weakening a full run. With neither set the executed command is exactly the
# historical `go test -race $$pkgs` over the whole filtered set. `-timeout 20m`
# is the one addition: go's default 10m-per-package kills
# internal/rules/sqlparse under -race on GitHub-hosted ubuntu-latest (observed
# 749s on main after #181). PKG overrides the package set; it is resolved
# through `go list` and re-filtered against projects/ so it can never reach a
# clone, and it is checked FAIL-CLOSED: a PKG that matches no in-module package
# is exit 2, never a silent "no packages to test, ok" that would read as green
# (the same hazard the go_pkgs empty-result check guards). RUN passes `-run` to
# narrow to matching tests within whatever package set is active, and is
# checked the same way and for the same reason (#278): `go test -run` prints
# `ok ... [no tests to run]` and exits 0 when the pattern matches nothing, so a
# mistyped RUN would otherwise report a green run over zero tests.
test:
	@$(go_pkgs); \
	if [ -n "$(PKG)" ]; then \
	  pkgs=$$(go list $(PKG) 2>/dev/null | grep -v "^$$module/$(PROJECTS_DIR)/"); \
	  if [ -z "$$pkgs" ]; then echo "make test: PKG=$(PKG) matched no packages in this module" >&2; exit 2; fi; \
	fi; \
	if [ -n "$(RUN)" ]; then \
	  n=$$(go test $$pkgs -list '$(RUN)' 2>/dev/null | grep -c '^Test'); \
	  if [ "$$n" -eq 0 ]; then echo "make test: RUN=$(RUN) matched no test in the selected packages — go test exits 0 over an empty -run selection, which reads as a green run" >&2; exit 2; fi; \
	fi; \
	go test -count=1 -timeout 20m -race $(if $(RUN),-run '$(RUN)',) $$pkgs

## vet: static analysis
vet:
	@$(go_pkgs); go vet $$pkgs

## fmt: fail if any TRACKED file is not gofmt-clean
# Tracked files only, deliberately: `gofmt -l .` is a plain filesystem walk with
# no notion of modules or .gitignore, so it descends into projects/ and fails on
# a cloned validating target's source. Keying off `git ls-files` excludes every
# gitignored path by construction, not by an exclusion list that would rot.
fmt:
	@files=$$(git ls-files '*.go') || { \
		echo "fmt: git ls-files failed, so the tracked file set is unknown — refusing to report a formatting pass over a file set this target never read" >&2; \
		exit 2; }; \
	if [ -z "$$files" ]; then \
		echo "fmt: git ls-files '*.go' matched nothing — a Go module with no tracked Go files means the LOOKUP is wrong, not that the formatting is clean" >&2; \
		exit 2; fi; \
	out=$$(gofmt -l $$files) || { \
		echo "fmt: gofmt could not judge the tracked files (absent, or a file it cannot parse)" >&2; \
		exit 2; }; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## check: self-host run — formwork checks this repo (must exit 0)
check:
	@$(call GO_RUN,./cmd/formwork,check)

## selftest: run every rule against its fixtures — repo self-host + each examples/ corpus (must exit 0)
selftest:
	@$(call GO_RUN,./cmd/formwork,test)
	@fail=0; for d in examples/*/; do \
		echo "==> formwork test -C $$d"; \
		go run ./cmd/formwork test -C "$$d" || fail=1; \
	done; exit $$fail

## lint: config self-integrity checks — repo self-host + each examples/ corpus (must exit 0)
# Loops the corpora the way `selftest` does. Until #89 this target ran at repo
# root ONLY, so the ported corpora — where a parity claim is proved before it is
# made — had never had fixture-coverage, empty-scope, exemption-hygiene or
# prefilter-load-bearing run against them at all.
#
# Wiring the loop needed a mechanism first, and the mechanism is deliberately NOT
# a flag here: the corpora are not uniformly small ports of a 13k-file tree —
# `palletra-port-full` carries no source tree at all, so a scope-reading check
# over it reports its whole domain instead of discriminating within it — and a
# skip list living in a Makefile recipe is an exemption nobody reviews. #324 is
# what the difference cost: for as long as that condition read as incidental,
# every number derived from it was quoted rather than re-derived. Each corpus
# declares its own in
# `.formwork/lint.yaml`, beside the rules it exempts, with the reason for each
# entry; `formwork lint` prints every skip on every run, and refuses (exit 2) a
# declared skip the run never reached — so this loop cannot quietly stop
# checking something.
#
# What the loop does NOT assert is that each corpus still has a falsifiable
# check left: lint refuses a run where NO check ran, but it cannot yet refuse
# one where only unfalsifiable checks did. `palletra-port-full` is the corpus
# where that gap would show, and this is its board:
#
#   formwork lint: 4/4 checks passed (2 skipped: empty-scope, exemption-hygiene — see .formwork/lint.yaml)
#
# Four checks run over a 704-rule corpus. rules-present cannot fail there, but
# the other three can: prose-not-truncated reads the rule files' own prose,
# fixture-coverage goes red the moment any rule's fixture tree is deleted, and
# prefilter-load-bearing goes red on a prefilter that narrows its rule. The two
# skips are declared with their reasons in that corpus's `.formwork/lint.yaml`.
#
# Every figure in that paragraph, and every figure in that `.formwork/lint.yaml`,
# is re-derived from the tree by `corpus-disclosure-proof` below, which reddens
# on the sentence this paragraph replaced (#325) three times over: on `skips
# four`, on a board written `1/1`, and on `707 rules`.
lint:
	@$(call GO_RUN,./cmd/formwork,lint)
	@fail=0; for d in examples/*/; do \
		echo "==> formwork lint -C $$d"; \
		go run ./cmd/formwork lint -C "$$d" || fail=1; \
	done; exit $$fail

## corpus-disclosure-proof: re-derive every figure the palletra-port-full disclosures state and refuse one the tree contradicts
# WHY THIS TARGET EXISTS. Three tracked files describe examples/palletra-port-full
# to a reader: that corpus's own `.formwork/lint.yaml`, the `lint:` and
# `quickstart-proof:` comments in this Makefile, and the empty-scope bullet in
# docs/specs/2026-07-09-formwork-design.md. Until this target none of them was
# derived from anything. Every figure in them was typed by hand, and every one of
# them has been wrong at least once (#325, #324, #263). The rule count read 707,
# then 702, and the tree agreed with neither. The board read `1/1`, then `2/2`,
# and the binary agreed with neither. exemption-hygiene read 995, then 993,
# against a re-derivation that matched neither. The fixture-coverage debt read 15
# problems over 8 rules against a measured 85 over 78. Every one of those
# corrections re-measured the line beside the wrong one and left the wrong one
# standing, and the design spec carried its own pair of them through all of it.
#
# THE ENGINE CANNOT HOLD IT. internal/scan/scan.go prunes any directory named
# .formwork at any depth, unconditionally and declarable in no rule's scope, so
# no rule in that corpus — or in this repository above it — can read that
# corpus's lint.yaml. A disclosure the engine cannot see has to be re-derived
# from outside, which is what this does, by the recipe that lint.yaml's own
# header prescribes: enumerate the rules, count the tracked and the source
# files, read the board and the scan width off the binary, split its findings by
# rule type, and re-run each declared skip's check against a copy of
# `.formwork/` carrying `skip: []`.
#
# BOTH DIRECTIONS, PER FIGURE, because "the number appears somewhere" is how
# this rotted four times. A disclosure must state the derived value at least
# once — a figure nobody discloses is not a disclosure — AND every occurrence of
# that figure's SHAPE must carry the derived value, so a second, stale copy of
# the same sentence is red rather than invisible. The comparison runs over a
# whitespace-normalised copy of each file, so a reflowed comment or a rewrapped
# YAML folded scalar is judged on its words rather than on its line breaks.
#
# AND ONE CLAIM THAT IS NOT A NUMBER (#324). What let the wrong figures stand for
# so long was a WORD, not a digit: every disclosure called this corpus a small
# part of a 13k-file tree, which made a structural condition read as incidental.
# It is not a part of that tree. It carries no source file at all, and while that
# holds, a scope-reading check over it reports 100% of its domain by
# construction — the count such a check produces measures the corpus's emptiness
# and not any rule's correctness, which is a different statement from "most
# scopes match nothing here" and has a different consequence. So while the
# DERIVED source-file count is zero, no declared disclosure may describe this
# corpus in the vocabulary of partial coverage. That vocabulary is listed in the
# recipe below rather than in this comment, because this comment is itself one of
# the scanned files and naming it here would make this paragraph its own finding;
# for the same reason the scan reads a Makefile's prose lines and never its
# TAB-indented recipe bodies, which are the checker rather than the claim. The
# arm is gated on the derived count rather than asserted flat: give this corpus a
# source tree and it turns itself off on the next run.
#
# AND THE TWO AGENT MANUALS ARE SUBJECTS (#324, #263.4). The last surviving
# false clause of #324 sat in AGENTS.md and in its published counterpart,
# tools/publication/public-AGENTS.md. Both told a reader that every one of the
# palletra-port corpora has a source tree to check its rules against — a
# distributive claim, false for the flagship, which is the corpus this whole
# target is about. Neither file was a subject, so nothing here reached them:
# AGENTS.md carried the exact wording the vocabulary arm bans and every run was
# green over it. Both are subjects now, and both state the DERIVED number of
# corpora that carry a source file rather than an "each" that cannot be checked.
#
# A DERIVED COUNT, NOT A NEW BANNED WORD, deliberately. "source tree" cannot go
# into the vocabulary arm: that arm runs over every subject, and the TRUE
# statements — this file's and the design spec's, that this corpus has no source
# tree at all — are spelled with the same words as the false ones, so the arm
# would fire on the sentences that got it right. A count is falsifiable in both
# directions instead, and it turns itself off the way the vocabulary arm does:
# give the flagship a source file and the derived number becomes every corpus,
# and a manual still stating anything less is red.
#
# AND WHAT THAT CORPUS SKIPS, in the manuals' own words (#263.4). AGENTS.md
# described its lint.yaml as carrying two DEBT entries over eight ported rules
# with no fixtures. Both halves were false by the time anyone read them: the
# entries had been paid off and deleted, and the debt they named had been
# re-measured at more than five times the size that sentence gave it. So the
# manuals now state the skip set the board prints, derived from the corpus's own
# lint.yaml on every run. Re-declare a skip and they are red; pay one off and
# they are red again.
#
# AND WHAT ITS BOARD READS (#263.4, second half). Naming the skip set is not the
# same assertion as naming the board, and the sentence that carried the false
# DEBT account was a coverage claim: it told a reader what this corpus's lint run
# is worth. A manual could restate that board with any ratio it liked — or state
# none at all, which is what both did — and every run here was green, because the
# only shapes reaching the manuals were the corpus-shape ones above. This is the
# shape that has been wrong most often anywhere in this repository: it read `1/1`
# in the corpus's own lint.yaml, then `2/2`, against a binary that agreed with
# neither (#325). Both manuals state the board line and its ratio now, on the
# same both-directions rule as every other figure: state it at least once, and
# carry the derived value at EVERY occurrence of its shape, so a second stale
# copy is red rather than invisible. Add a check to the board or take one away
# and both manuals are red until they are re-derived.
#
# THE SUBJECT SET IS DERIVED FROM GIT, WITH A FLOOR, because of the cut. The OSS
# cut drops tools/publication and MATERIALISES AGENTS.md from public-AGENTS.md,
# so the public manual is not a path that exists on both sides — and this target
# is a `verify` prerequisite the cut keeps, where declaring it flat would be exit
# 2 on the missing-subject refusal below. The four subjects that cross are
# therefore asserted unconditionally, and the public manual is a subject exactly
# while git tracks it. That is keyed on the tree's index, not on what this target
# would prefer to find: a tracked public manual that is missing or empty is still
# exit 2, and on the far side of the cut the file it was scrubbed into is
# AGENTS.md, asserted unconditionally, carrying the same derived figures.
#
# AND IT REFUSES A SUBJECT IT CANNOT READ, the way `fmt` does. A declared
# disclosure file that is missing or empty, no rules enumerated, no tracked
# files, no board line, no scan summary, a findings total the JSON report cannot
# reconstruct rule-by-rule, a finding whose rule type falls outside the two the
# disclosure splits it into, no declared skip, a declared skip count the board
# disagrees with, or a skip whose check reports nothing once the skip is gone:
# each is exit 2 rather than a quiet pass.
corpus-disclosure-proof:
	@corpus=examples/palletra-port-full; \
	yaml="$$corpus/.formwork/lint.yaml"; \
	spec=docs/specs/2026-07-09-formwork-design.md; \
	pubmanual=tools/publication/public-AGENTS.md; \
	disclosures="$$yaml Makefile $$spec AGENTS.md"; \
	manuals="AGENTS.md"; \
	pubtracked=$$(git ls-files -- "$$pubmanual") || { echo "corpus-disclosure-proof: git ls-files failed, so this target cannot derive which agent manuals this tree carries — refusing rather than judging a subject set it could not derive" >&2; exit 2; }; \
	if [ -n "$$pubtracked" ]; then disclosures="$$disclosures $$pubmanual"; manuals="$$manuals $$pubmanual"; fi; \
	for f in $$disclosures; do \
		test -s "$$f" || { echo "corpus-disclosure-proof: declared disclosure $$f is missing or empty — there is nothing to judge there, and a green over it would be a pass over nothing" >&2; exit 2; }; \
	done; \
	tmp=$$(mktemp -d "$${TMPDIR:-/tmp}/formwork-disclosure.XXXXXX") || exit 2; \
	trap 'rm -rf "$$tmp"' EXIT HUP INT TERM; \
	norm() { sed -e 's/^[[:space:]]*//' -e 's/^#[[:space:]]*//' -e 's/^[[:space:]]*//' "$$1" | tr '\n' ' ' | tr -s ' '; }; \
	norm "$$yaml" > "$$tmp/yaml.norm"; \
	norm Makefile > "$$tmp/make.norm"; \
	norm "$$spec" > "$$tmp/spec.norm"; \
	fail=0; \
	claim() { \
		f="$$1"; label="$$2"; any="$$3"; want="$$4"; \
		n_any=$$(grep -oE "$$any" "$$f" | wc -l | tr -d ' '); \
		n_want=$$(grep -oF "$$want" "$$f" | wc -l | tr -d ' '); \
		if [ "$$n_want" -ge 1 ] && [ "$$n_any" -eq "$$n_want" ]; then printf '  ok    %-34s [%s]\n' "$$label" "$$want"; return 0; fi; \
		fail=1; \
		if [ "$$n_any" -eq 0 ]; then \
			printf '  FAIL  %-34s states no figure of this shape at all; re-derived from the tree it is [%s]\n' "$$label" "$$want" >&2; \
		else \
			printf '  FAIL  %-34s states [%s]; re-derived from the tree it is [%s]\n' "$$label" "$$(grep -oE "$$any" "$$f" | sort -u | tr '\n' ' ')" "$$want" >&2; \
		fi; \
		return 0; \
	}; \
	echo "==> corpus-disclosure-proof: re-deriving $$corpus"; \
	go run ./cmd/formwork list -C "$$corpus" rules > "$$tmp/rules.txt" || { echo "corpus-disclosure-proof: could not enumerate the corpus's rules, so nothing below can be derived" >&2; exit 2; }; \
	rules=$$(wc -l < "$$tmp/rules.txt" | tr -d ' '); \
	[ "$$rules" -gt 0 ] || { echo "corpus-disclosure-proof: the corpus enumerates ZERO rules — refusing to judge a disclosure against an empty corpus" >&2; exit 2; }; \
	cmdrules=$$(awk -F'\t' '$$2 == "command"' "$$tmp/rules.txt" | wc -l | tr -d ' '); \
	git ls-files "$$corpus" > "$$tmp/tracked.txt" || { echo "corpus-disclosure-proof: git ls-files failed, so the file counts this disclosure states cannot be derived" >&2; exit 2; }; \
	tracked=$$(wc -l < "$$tmp/tracked.txt" | tr -d ' '); \
	[ "$$tracked" -gt 0 ] || { echo "corpus-disclosure-proof: git ls-files $$corpus matched nothing — refusing" >&2; exit 2; }; \
	srcfiles=$$(grep -vc '/\.formwork/' "$$tmp/tracked.txt" || true); \
	git ls-files -- 'examples/palletra-port-*' > "$$tmp/corpora.txt" || { echo "corpus-disclosure-proof: git ls-files failed over examples/palletra-port-*, so the count of corpora the agent manuals state cannot be derived" >&2; exit 2; }; \
	ncorpora=$$(sed 's|^\(examples/[^/]*\)/.*|\1|' "$$tmp/corpora.txt" | sort -u | grep -c . || true); \
	[ "$$ncorpora" -ge 1 ] || { echo "corpus-disclosure-proof: git tracks no examples/palletra-port-* corpus at all, so a figure derived over them would describe nothing" >&2; exit 2; }; \
	grep -q "^$$corpus/" "$$tmp/corpora.txt" || { echo "corpus-disclosure-proof: $$corpus is not among the examples/palletra-port-* corpora git tracks, so a count derived over them says nothing about the corpus this target judges" >&2; exit 2; }; \
	srccorpora=$$(grep -v '/\.formwork/' "$$tmp/corpora.txt" | sed 's|^\(examples/[^/]*\)/.*|\1|' | sort -u | grep -c . || true); \
	go run ./cmd/formwork check -C "$$corpus" --format json > "$$tmp/check.json" 2> "$$tmp/check.err" || true; \
	scanned=$$(sed -n 's/^ *"files_scanned": \([0-9][0-9]*\),*$$/\1/p' "$$tmp/check.json"); \
	case "$$scanned" in "" | *[!0-9]*) echo "corpus-disclosure-proof: formwork check -C $$corpus reported no single files_scanned count, so the width the findings below were produced over cannot be derived; it said: $$(tail -2 "$$tmp/check.err" | tr '\n' ' ')" >&2; exit 2;; esac; \
	findings=$$(sed -n 's/^ *"findings": \([0-9][0-9]*\),*$$/\1/p' "$$tmp/check.json"); \
	case "$$findings" in "" | *[!0-9]*) echo "corpus-disclosure-proof: formwork check -C $$corpus reported no single findings total, so nothing this repository says about its findings can be derived; it said: $$(tail -2 "$$tmp/check.err" | tr '\n' ' ')" >&2; exit 2;; esac; \
	grep -oE '^ +"rule": "[^"]+"' "$$tmp/check.json" | sed -e 's/.*: "//' -e 's/"$$//' > "$$tmp/findrules.txt"; \
	nfr=$$(grep -c . "$$tmp/findrules.txt" || true); \
	[ "$$nfr" -eq "$$findings" ] || { echo "corpus-disclosure-proof: the JSON report names $$nfr finding rule(s) where the same run totalled $$findings — refusing to split a total this target cannot reconstruct rule by rule" >&2; exit 2; }; \
	cmdfind=$$(awk -F'\t' 'NR==FNR{t[$$1]=$$2; next} t[$$1]=="command"' "$$tmp/rules.txt" "$$tmp/findrules.txt" | grep -c . || true); \
	pcfind=$$(awk -F'\t' 'NR==FNR{t[$$1]=$$2; next} t[$$1]=="pattern-count"' "$$tmp/rules.txt" "$$tmp/findrules.txt" | grep -c . || true); \
	[ $$((cmdfind + pcfind)) -eq "$$findings" ] || { echo "corpus-disclosure-proof: the two rule types this corpus's disclosure splits its findings into no longer account for all of them (command $$cmdfind + pattern-count $$pcfind against $$findings) — re-derive the split before restating it" >&2; exit 2; }; \
	withpath=$$(grep -cE '^ +"path": ' "$$tmp/check.json" || true); \
	nopath=$$((findings - withpath)); \
	go run ./cmd/formwork lint -C "$$corpus" > "$$tmp/board.txt" 2> "$$tmp/board.err" || true; \
	board=$$(tail -1 "$$tmp/board.txt"); \
	ratio=$$(printf '%s\n' "$$board" | sed -n 's/^formwork lint: \([0-9][0-9]*\/[0-9][0-9]*\) checks passed.*/\1/p'); \
	[ -n "$$ratio" ] || { echo "corpus-disclosure-proof: the last line of formwork lint -C $$corpus is not a board line — [$$board]; the run said: $$(tail -2 "$$tmp/board.err" | tr '\n' ' ')" >&2; exit 2; }; \
	boardskips=$$(printf '%s\n' "$$board" | sed -n 's/.*(\([0-9][0-9]*\) skipped:.*/\1/p'); \
	[ -n "$$boardskips" ] || boardskips=0; \
	skips=$$(grep -oE '^ *- check: [a-z][a-z-]*' "$$yaml" | awk '{print $$3}'); \
	nskips=$$(printf '%s\n' "$$skips" | grep -c . || true); \
	[ "$$nskips" -ge 1 ] || { echo "corpus-disclosure-proof: $$yaml declares no skip. This target exists to re-derive the numbers those skip reasons state, so with none declared it would report a pass over nothing — retire it or repoint it" >&2; exit 2; }; \
	[ "$$nskips" -eq "$$boardskips" ] || { echo "corpus-disclosure-proof: $$yaml declares $$nskips skip(s) but the board reports $$boardskips skipped — they disagree before any figure is compared" >&2; exit 2; }; \
	skiplist=$$(printf '%s\n' $$skips | sort | awk 'NR > 1 { printf ", " } { printf "%s", $$0 } END { print "" }'); \
	mkdir -p "$$tmp/noskip"; \
	cp -R "$$corpus/.formwork" "$$tmp/noskip/" || exit 2; \
	printf 'version: 1\nskip: []\n' > "$$tmp/noskip/.formwork/lint.yaml"; \
	go run ./cmd/formwork lint -C "$$tmp/noskip" > "$$tmp/noskip.txt" 2> "$$tmp/noskip.err" || true; \
	grep -q '^formwork lint: ' "$$tmp/noskip.txt" || { echo "corpus-disclosure-proof: the skip-free re-run printed no board line, so no skip's problem count can be derived; it said: $$(tail -2 "$$tmp/noskip.err" | tr '\n' ' ')" >&2; exit 2; }; \
	printf 'derived: %s rules, %s of them command rules; %s tracked file(s); %s source file(s)\n' "$$rules" "$$cmdrules" "$$tracked" "$$srcfiles"; \
	printf 'derived: %s\n' "$$board"; \
	printf 'derived: %s of %s palletra-port corpora carry a source file; the corpus declares %s skip(s): %s\n' "$$srccorpora" "$$ncorpora" "$$nskips" "$$skiplist"; \
	claim "$$tmp/yaml.norm" "lint.yaml: board line" "formwork lint: [0-9]+/[0-9]+ checks passed \([0-9]+ skipped: [^)]*\)" "$$board"; \
	claim "$$tmp/make.norm" "Makefile: board line" "formwork lint: [0-9]+/[0-9]+ checks passed \([0-9]+ skipped: [^)]*\)" "$$board"; \
	claim "$$tmp/yaml.norm" "lint.yaml: checks-passed ratio" "[0-9]+/[0-9]+ checks passed" "$$ratio checks passed"; \
	claim "$$tmp/make.norm" "Makefile: checks-passed ratio" "[0-9]+/[0-9]+ checks passed" "$$ratio checks passed"; \
	claim "$$tmp/yaml.norm" "lint.yaml: rule count" " [0-9]+-rule corpus" " $$rules-rule corpus"; \
	claim "$$tmp/make.norm" "Makefile: rule count" " [0-9]+-rule corpus" " $$rules-rule corpus"; \
	claim "$$tmp/yaml.norm" "lint.yaml: command-rule count" " [0-9]+ command rules" " $$cmdrules command rules"; \
	claim "$$tmp/yaml.norm" "lint.yaml: tracked-file count" "wc -l -> [0-9]+" "wc -l -> $$tracked"; \
	claim "$$tmp/yaml.norm" "lint.yaml: source-file count" "grep -vc [^ ]+ -> [0-9]+" "grep -vc '/\.formwork/' -> $$srcfiles"; \
	claim "$$tmp/yaml.norm" "lint.yaml: scan width" "scan: [0-9]+ file\(s\) scanned" "scan: $$scanned file(s) scanned"; \
	claim "$$tmp/make.norm" "Makefile: scan width" "scan: [0-9]+ file\(s\) scanned" "scan: $$scanned file(s) scanned"; \
	claim "$$tmp/make.norm" "Makefile: findings total" "[0-9]+ finding" "$$findings finding"; \
	claim "$$tmp/make.norm" "Makefile: findings from command" "[0-9]+ are command-rule findings" "$$cmdfind are command-rule findings"; \
	claim "$$tmp/make.norm" "Makefile: findings from pattern-count" "[0-9]+ are pattern-count findings" "$$pcfind are pattern-count findings"; \
	claim "$$tmp/make.norm" "Makefile: findings naming no file" "all [0-9]+ carry an empty" "all $$nopath carry an empty"; \
	claim "$$tmp/spec.norm" "spec: rule count" "carries [0-9]+ rules" "carries $$rules rules"; \
	claim "$$tmp/spec.norm" "spec: non-external-tool rules" "[0-9]+ of its non-external-tool rules" "$$((rules - cmdrules)) of its non-external-tool rules"; \
	for m in $$manuals; do \
		norm "$$m" > "$$tmp/manual.norm"; \
		claim "$$tmp/manual.norm" "$$m: board line" "formwork lint: [0-9]+/[0-9]+ checks passed \([0-9]+ skipped: [^)]*\)" "$$board"; \
		claim "$$tmp/manual.norm" "$$m: checks-passed ratio" "[0-9]+/[0-9]+ checks passed" "$$ratio checks passed"; \
		claim "$$tmp/manual.norm" "$$m: corpora with a source tree" "[0-9]+ of the [0-9]+ palletra-port corpora carry a source tree" "$$srccorpora of the $$ncorpora palletra-port corpora carry a source tree"; \
		claim "$$tmp/manual.norm" "$$m: the corpus's declared skips" "[0-9]+ skipped: [a-z][a-z-]*(, [a-z][a-z-]*)*" "$$boardskips skipped: $$skiplist"; \
	done; \
	for s in $$skips; do \
		n=$$(awk -v c="$$s" '$$0 ~ "^\\[" c "\\] FAIL" { for (i = 1; i <= NF; i++) if ($$i == "problem(s)") print $$(i-1) }' "$$tmp/noskip.txt"); \
		if [ -z "$$n" ]; then \
			printf '  FAIL  %-34s with that skip removed the check reports NO problems, so the reason states a number that measures nothing — delete the skip or restate it\n' "lint.yaml: skip $$s" >&2; \
			fail=1; \
			continue; \
		fi; \
		claim "$$tmp/yaml.norm" "lint.yaml: skip $$s" "$$s reports [0-9]+ problems" "$$s reports $$n problems"; \
		if [ "$$s" = "empty-scope" ] && [ "$$n" -ne $$((rules - cmdrules)) ]; then \
			printf '  FAIL  %-34s the reason derives this count as the corpus rule count minus its command rules (%s - %s = %s); the check reports %s\n' "lint.yaml: skip $$s" "$$rules" "$$cmdrules" "$$((rules - cmdrules))" "$$n" >&2; \
			fail=1; \
		fi; \
	done; \
	vocab='(^|[^a-zA-Z])(slice|slices)([^a-zA-Z]|$$)|source subset|subset of (a|an|the|this|that) ([a-z0-9-]+ )*(tree|repo|repository)'; \
	if [ "$$srcfiles" -ne 0 ]; then \
		printf '  ok    %-34s [not asserted: %s source file(s) — a disclosure calling this corpus part of a larger tree could now be true]\n' "vocabulary: all disclosures" "$$srcfiles"; \
	else \
		for f in $$disclosures; do \
			case "$$f" in Makefile) awk '!/^\t/' "$$f";; *) cat "$$f";; esac \
				| sed -e 's/^[[:space:]]*//' -e 's/^#[[:space:]]*//' | tr '\n' ' ' | tr -s ' ' > "$$tmp/vocab.one"; \
			hits=$$(grep -oiE "$$vocab" "$$tmp/vocab.one" | grep -c . || true); \
			if [ "$$hits" -eq 0 ]; then \
				printf '  ok    %-34s [none, over %s source file(s)]\n' "vocabulary: $$f" "$$srcfiles"; \
			else \
				fail=1; \
				printf '  FAIL  %-34s %s occurrence(s) of wording this corpus does not earn: it carries %s source file(s), so it is not part of a larger tree and every scope-reading check over it reports 100%% of its domain by construction (#324). Located:\n' "vocabulary: $$f" "$$hits" "$$srcfiles" >&2; \
				case "$$f" in Makefile) awk '!/^\t/ { print FNR ": " $$0 }' "$$f";; *) awk '{ print FNR ": " $$0 }' "$$f";; esac \
					| grep -iE "$$vocab" > "$$tmp/vocab.lines" || true; \
				sed "s|^|          $$f:|" "$$tmp/vocab.lines" >&2; \
				located=$$(grep -oiE "$$vocab" "$$tmp/vocab.lines" | grep -c . || true); \
				[ "$$located" -ge "$$hits" ] || printf '          (%s of them wrap across a line break and are counted from the whitespace-normalised copy, not located)\n' "$$((hits - located))" >&2; \
			fi; \
		done; \
	fi; \
	if [ "$$fail" -ne 0 ]; then \
		echo "corpus-disclosure-proof: FAILED — a disclosure disagrees with the tree it describes. Re-derive with the recipe in $$yaml's header and restate every place that figure appears; do not copy a number out of the sentence beside it." >&2; \
		exit 1; \
	fi; \
	echo "corpus-disclosure-proof: OK — every disclosed figure re-derived from the tree"


## quickstart-proof: the teaching corpus governs its own tree, and every figure stated about it — here and in docs/quickstart.md — is re-derived
# `lint` and `selftest` both loop the corpora now (#89/#39). `check` still does
# not, and the asymmetry is deliberate — but not for the reason this comment
# gave until #324. It said palletra-port-full's TREE legitimately carries 91
# findings. That tree carries nothing: `formwork check -C
# examples/palletra-port-full` opens with `scan: 0 file(s) scanned`, and every
# figure in the next two sentences is re-derived from that same run by
# `corpus-disclosure-proof` above. Of the 91 findings, 87 are command-rule
# findings — each one a detector this corpus does not carry and cannot, so the
# process exits non-zero before reading a byte — and 4 are pattern-count
# findings, minimums a tree with no files cannot meet; all 91 carry an empty
# path, so not one of them is about a file. Gating that would assert an
# invariant about a missing source tree rather than about the corpus's rules.
# `test` — does each rule discriminate on its own fixtures — and a check set
# each corpus selects for itself are the right questions there.
#
# examples/quickstart is different in kind: it is the worked example
# docs/quickstart.md walks a reader through, so it DOES claim to be exemplary,
# and a reader who follows the doc runs exactly the two commands this target
# runs first. Both are clean: check exit 0, and quickstart lint 7/7.
#
# The specific claim this pins is in CLAUDE.md: quickstart "carries a `_test.go`
# file on purpose: without it the `**/*_test.go` excludes match nothing and
# `formwork lint` reports them as dead escape hatches, so that file is what makes
# the exclusions honest" (#55's exemption-hygiene). That was prose with no pin,
# and then it was prose beside a mutation nobody ran: remove
# examples/quickstart/internal/handler/orders_test.go and the board drops to 6/7,
# on 3 dead exclusions — handlers-stay-small, no-panic-in-request-path,
# no-print-debugging. This target performs that removal on a copy every run and
# reads the three names back out of it, so the claim is executed rather than
# recorded.
#
# THE DENOMINATOR MOVES whenever a lint check is added, and this note is where
# that cost was paid twice. It went 5 -> 6 when rules-present landed (#151),
# which stale-dated the note within a day of it being written; the correction
# that followed restated one figure, left the other beside it standing, and
# advised the reader to re-derive by hand — so when the two lane checks took the
# board to 7 the note was wrong in both halves again, reading `lint 6/6` in one
# sentence and `not 5/6` in the next (#325's class, one target down from where
# #325 found it). Nothing above is quoted now. Every figure in this paragraph
# and the one before it is re-derived from the corpus on every run by the recipe
# below, in both directions, so a figure that moves fails here instead of
# rotting into the next correction.
#
# AND IT REFUSES A SUBJECT IT CANNOT READ, the way `corpus-disclosure-proof`
# does. Every state in which it cannot read what it is judging is exit 2 rather
# than a quiet pass. They are named here and not counted, because a count of
# them is one more hand-typed figure with nothing deriving it, which is the
# defect this whole target exists to stop. Each was driven rather than reasoned
# about: the pinned test file already absent (proved by
# removing it while declaring the exemption-hygiene skip, so the clean run stays
# green and the refusal is what speaks); either lint run printing no board line
# this target can read (proved against a stand-in binary that answers the clean
# corpus normally and the throwaway copy with prose); and a drop that names no
# dead exclusion (same stand-in, a board line with nothing under it). The board
# line one is load-bearing rather than decorative: an unreadable board leaves
# the derived value empty, and an empty value would satisfy the comparison
# below by matching the prefix of the sentence it is meant to judge.
#
# The removal itself is refused when it changes nothing (exit 1, proved by
# adding a second `_test.go` beside the pinned one, which keeps the exclusions
# alive and leaves the board where it was). That is the arm that stops this
# target reporting a pass over a mutation that stopped mutating.
#
# AND IT JUDGES THE WALKTHROUGH, not only the corpus. docs/quickstart.md §9 is
# the first lint output an adopter ever sees and the thing they compare their
# own terminal against, so a check the tool gains and the document does not is a
# reader being told a line they are looking at should not be there. That is what
# had happened: prose-not-truncated (#59) runs on every corpus carrying rules,
# and the document showed neither the transcript line nor a table row for it,
# and put the board at 4/6 where the tool prints 5/7. The check-name set and the
# denominator are derived from the lint run above; the numerator is that
# denominator minus the FAIL blocks the document itself shows, so the transcript
# must be internally consistent as well as true; and §3's two figures come from
# the check run, which the document says outright IS this corpus.
#
# The coupling that makes this derivable is worth naming: examples/quickstart
# and the repository the guide has the reader build run the same check set —
# both declare lanes, neither declares a prefilter. Land a prefilter in the
# teaching corpus and prefilter-load-bearing runs here and not there, and this
# arm goes red. That red is correct: it says the worked example and the
# walkthrough have diverged, which is the one thing the document promises.
quickstart-proof:
	@corpus=examples/quickstart; \
	pin=internal/handler/orders_test.go; \
	tmp=$$(mktemp -d "$${TMPDIR:-/tmp}/formwork-quickstart.XXXXXX") || exit 2; \
	trap 'rm -rf "$$tmp"' EXIT HUP INT TERM; \
	fail=0; \
	claim() { \
		f="$$1"; label="$$2"; any="$$3"; want="$$4"; \
		n_any=$$(grep -oE "$$any" "$$f" | wc -l | tr -d ' '); \
		n_want=$$(grep -oF "$$want" "$$f" | wc -l | tr -d ' '); \
		if [ "$$n_want" -ge 1 ] && [ "$$n_any" -eq "$$n_want" ]; then printf '  ok    %-34s [%s]\n' "$$label" "$$want"; return 0; fi; \
		fail=1; \
		if [ "$$n_any" -eq 0 ]; then \
			printf '  FAIL  %-34s states no figure of this shape at all; re-derived from the tree it is [%s]\n' "$$label" "$$want" >&2; \
		else \
			printf '  FAIL  %-34s states [%s]; re-derived from the tree it is [%s]\n' "$$label" "$$(grep -oE "$$any" "$$f" | sort -u | tr '\n' ' ')" "$$want" >&2; \
		fi; \
		return 0; \
	}; \
	sed -e 's/^[[:space:]]*//' -e 's/^#[[:space:]]*//' -e 's/^[[:space:]]*//' Makefile | tr '\n' ' ' | tr -s ' ' > "$$tmp/make.norm"; \
	echo "==> quickstart-proof: re-deriving $$corpus"; \
	go run ./cmd/formwork check -C "$$corpus" > "$$tmp/check.txt" 2>&1; checkrc=$$?; \
	scanned=$$(sed -n 's/^scan: \([0-9][0-9]*\) file(s) scanned.*/\1/p' "$$tmp/check.txt" | head -1); \
	rulesratio=$$(sed -n 's/^formwork: \([0-9][0-9]*\/[0-9][0-9]*\) rules passed.*/\1/p' "$$tmp/check.txt" | head -1); \
	go run ./cmd/formwork lint -C "$$corpus" > "$$tmp/lint.txt" 2>&1; lintrc=$$?; \
	if [ "$$checkrc" -ne 0 ] || [ "$$lintrc" -ne 0 ]; then \
		echo "quickstart-proof: the teaching corpus does not govern its own tree cleanly — check exited $$checkrc, lint exited $$lintrc" >&2; \
		cat "$$tmp/check.txt" "$$tmp/lint.txt" >&2; exit 1; \
	fi; \
	board=$$(grep '^formwork lint: ' "$$tmp/lint.txt" | tail -1); \
	ratio=$$(printf '%s\n' "$$board" | sed -n 's/^formwork lint: \([0-9][0-9]*\/[0-9][0-9]*\).*/\1/p'); \
	[ -n "$$ratio" ] || { echo "quickstart-proof: formwork lint -C $$corpus printed no board line — [$$board]; nothing this target claims about the board can be derived" >&2; exit 2; }; \
	test -f "$$corpus/$$pin" || { echo "quickstart-proof: $$corpus/$$pin is not there, and its ABSENCE is what this proof measures — restore it or repoint this target" >&2; exit 2; }; \
	mkdir -p "$$tmp/mut"; \
	cp -R "$$corpus/." "$$tmp/mut/" || exit 2; \
	rm "$$tmp/mut/$$pin" || exit 2; \
	go run ./cmd/formwork lint -C "$$tmp/mut" > "$$tmp/mut.txt" 2>&1 || true; \
	mutboard=$$(grep '^formwork lint: ' "$$tmp/mut.txt" | tail -1); \
	mutratio=$$(printf '%s\n' "$$mutboard" | sed -n 's/^formwork lint: \([0-9][0-9]*\/[0-9][0-9]*\).*/\1/p'); \
	[ -n "$$mutratio" ] || { echo "quickstart-proof: the run without $$pin printed no board line, so the drop this target claims cannot be derived; it said: $$(tail -2 "$$tmp/mut.txt" | tr '\n' ' ')" >&2; exit 2; }; \
	[ "$$mutratio" != "$$ratio" ] || { echo "quickstart-proof: removing $$pin left the board at $$ratio. That file is carried ON PURPOSE to keep the **/*_test.go exclusions honest, and with it gone nothing moved — the claim it makes them honest is measuring nothing" >&2; exit 1; }; \
	sed -n 's/^ *\([a-z][a-z0-9-]*\): scope.exclude "[^"]*" matches no files.*/\1/p' "$$tmp/mut.txt" | sort -u > "$$tmp/dead.txt"; \
	ndead=$$(grep -c . "$$tmp/dead.txt" || true); \
	[ "$$ndead" -ge 1 ] || { echo "quickstart-proof: the board dropped to $$mutratio without $$pin but named no dead exclusion, so this target cannot say which rules that file keeps honest" >&2; exit 2; }; \
	printf 'derived: quickstart lint %s clean; %s without %s, on %s dead exclusion(s)\n' "$$ratio" "$$mutratio" "$$pin" "$$ndead"; \
	claim "$$tmp/make.norm" "quickstart: clean board" "quickstart lint [0-9]+/[0-9]+" "quickstart lint $$ratio"; \
	claim "$$tmp/make.norm" "quickstart: board without the test" "drops to [0-9]+/[0-9]+" "drops to $$mutratio"; \
	claim "$$tmp/make.norm" "quickstart: dead exclusions" "on [0-9]+ dead exclusion" "on $$ndead dead exclusion"; \
	claim "$$tmp/make.norm" "quickstart: check exit status" "check exit [0-9]+" "check exit $$checkrc"; \
	while read -r r; do \
		if grep -Fq "$$r" "$$tmp/make.norm"; then \
			printf '  ok    %-34s [%s]\n' "quickstart: names the exclusion" "$$r"; \
		else \
			printf '  FAIL  %-34s the run names %s as an exclusion %s keeps honest; this Makefile does not name it\n' "quickstart: names the exclusion" "$$r" "$$pin" >&2; \
			fail=1; \
		fi; \
	done < "$$tmp/dead.txt"; \
	doc=docs/quickstart.md; \
	test -s "$$doc" || { echo "quickstart-proof: $$doc is missing or empty, and it is the document this corpus is the worked example of — there is nothing to judge" >&2; exit 2; }; \
	sed -n 's/^\[\([a-z][a-z0-9-]*\)\].*/\1/p' "$$tmp/lint.txt" | sort -u > "$$tmp/checks.txt"; \
	nchecks=$$(grep -c . "$$tmp/checks.txt" || true); \
	[ "$$nchecks" -ge 1 ] || { echo "quickstart-proof: the lint run over $$corpus named no check at all, so what $$doc says lint runs cannot be compared with what it runs" >&2; exit 2; }; \
	[ "$$nchecks" -eq "$${ratio#*/}" ] || { echo "quickstart-proof: counted $$nchecks check line(s) against a board reading $$ratio — this target is parsing the run wrong and would judge $$doc against a number it invented" >&2; exit 2; }; \
	docratio=$$(sed -n 's/^formwork lint: \([0-9][0-9]*\/[0-9][0-9]*\) checks passed.*/\1/p' "$$doc" | head -1); \
	[ -n "$$docratio" ] || { echo "quickstart-proof: $$doc shows no lint board line, so the walkthrough a first-time adopter compares their own output against cannot be judged" >&2; exit 2; }; \
	docfails=$$(grep -cE '^\[[a-z][a-z0-9-]*\] FAIL' "$$doc" || true); \
	claim "$$doc" "quickstart.md: lint board" "formwork lint: [0-9]+/[0-9]+ checks passed" "formwork lint: $$((nchecks - docfails))/$$nchecks checks passed"; \
	claim "$$doc" "quickstart.md: scan width" "scan: [0-9]+ file\(s\) scanned" "scan: $$scanned file(s) scanned"; \
	claim "$$doc" "quickstart.md: rules passed" "formwork: [0-9]+/[0-9]+ rules passed" "formwork: $$rulesratio rules passed"; \
	while read -r c; do \
		grep -qE "^\[$$c\] " "$$doc" || { printf '  FAIL  %-34s lint prints [%s] over %s and the walkthrough transcript does not show it — a reader comparing their own output against the guide finds a line the guide does not account for\n' "quickstart.md: transcript" "$$c" "$$corpus" >&2; fail=1; }; \
		grep -qE "^\| .$$c. \|" "$$doc" || { printf '  FAIL  %-34s the check table omits %s, so the table claims to enumerate what lint catches and does not\n' "quickstart.md: check table" "$$c" >&2; fail=1; }; \
	done < "$$tmp/checks.txt"; \
	if [ "$$fail" -ne 0 ]; then \
		echo "quickstart-proof: FAILED — what this target says about the teaching corpus disagrees with the corpus. Re-derive by running the two commands above and the removal below them; do not copy a number out of the sentence beside it." >&2; \
		exit 1; \
	fi; \
	echo "quickstart-proof: OK — the teaching corpus governs its own tree cleanly, and every figure stated about it is re-derived"





# CUT_PROOF_DEST is where publication-cut-proof builds its throwaway tree.
# Under TMPDIR and deliberately NOT in the worktree: an untracked directory
# here would be visible to the `check` and `selftest` runs happening inside
# this very invocation, which is the trap the gate log already carries for the
# `gate` target's own log file.
CUT_PROOF_DEST ?= $(if $(TMPDIR),$(TMPDIR),/tmp)/formwork-cut-proof




## hooks-e2e-proof: the built binary's git hooks gate a real commit, by the rule
# No Makefile target ran the `formwork hooks` COMMAND until this one.
# internal/hooks/commit_test.go already drives real commits against a built
# binary, but through hooks.Install and hooks.Verify in-process; the subcommand
# an operator actually types — its flags, its exit codes, and a rule YAML decoded
# through the type registry — was reached by nothing. The proof builds the
# binary, copies examples/quickstart into a throwaway repo with a throwaway HOME,
# installs, and drives real `git commit`s through the shim git executes.
#
# It asserts the violating commit was refused BY THE RULE, never merely that it
# was refused. Measured: with the binary off PATH the shim exits 127, the commit
# is blocked, and a status-only assertion goes green over a gate that never ran.
#
# THE CORPUS IS THE COVERAGE. quickstart declares five rules across four types
# over real Go and SQL, and the proof drives one commit per type: a
# forbidden-pattern and a required-pattern refused, a pair-consistency refused
# over a migration, and a warn-severity file-size that reports and lets the
# commit land. Two sentences up were false for a year (#318) — the shell-to-Go
# port swapped the corpus for a two-file synthetic one (one forbidden-pattern
# rule over one .txt) and the throwaway HOME for GIT_CONFIG_GLOBAL, and carried
# this comment across unchanged, so the record claimed four rule types it had
# stopped reaching. The test now reads this block and compares the corpus named
# here with the one it runs, which is why the claim above is checkable rather
# than merely written down.
#
# The throwaway HOME is load-bearing rather than decoration: git reads its
# global EXCLUDES file from XDG_CONFIG_HOME/HOME whatever GIT_CONFIG_GLOBAL
# says, so without it a developer's own global ignores decide which corpus
# files `git add -A` puts into the repository under test.
hooks-e2e-proof:
	@$(call proof,./internal/repoproof,TestInstall)








## sync-proof: the clone harness refuses a manifest that declares no targets
# Needs no network and no clone: the accept-vector builds a throwaway local
# git repo. Load-bearing for the OSS cut — per #114 the mechanism crosses but
# repos.txt does not, so the public tree's first `make sync` sees no manifest.
sync-proof:
	@$(call proof,./internal/repoproof,TestSync)

## gate-proof: `make gate`'s verdict and exit status survive a pipe, both directions
# The anti-footgun has to be gated or it rots exactly like the deny hook did.
# Hermetic and instant: the proof drives `gate` through GATE_CMD against
# stand-ins that pass and fail on demand, so it never runs the real suite — and
# it pins both directions, because a negative-only proof passes just as well
# against a gate that always fails.
gate-proof:
	@$(call proof,./internal/repoproof,TestGate)

##@ Validating targets (read-only clones under projects/)

## sync: check out the validating targets in repos.txt at their pinned refs
# projects/ is gitignored — nothing here is ever committed. The checkout moves
# ONLY to the ref pinned in repos.txt: no pull, no branch following. Advancing
# the reference is an edit to repos.txt, so it lands as a reviewable commit
# instead of happening as a side effect of running this target.
# Fail-closed throughout (the repo's exit-code posture): an unknown ref or a
# dirty tree stops the run rather than silently checking out something else —
# and so does a manifest that declares nothing. A bare read loop over an empty
# or absent manifest runs zero times and exits 0, which tells the operator the
# sync succeeded in the only vocabulary this target has. That matters beyond
# theory: per #114 the mechanism crosses into the OSS repo but repos.txt does
# not, so the public tree's first `make sync` runs against no manifest at all.
# internal/repoproof's TestSync pins both directions.
sync:
	@if [ ! -r "$(REPOS_FILE)" ]; then \
		echo "$(REPOS_FILE): no manifest, so no targets to sync" >&2; \
		echo "  declare one per line as '<name> <url> <pinned-ref>'" >&2; \
		exit 2; fi
	@mkdir -p $(PROJECTS_DIR)
	@n=0; while IFS=' ' read -r name url ref; do \
		case "$$name" in ""|"#"*) continue ;; esac; \
		n=$$((n + 1)); \
		dir="$(PROJECTS_DIR)/$$name"; \
		if [ -z "$$ref" ]; then echo "$$name: no pinned ref in $(REPOS_FILE)" >&2; exit 1; fi; \
		if [ ! -d "$$dir/.git" ]; then \
			echo "# $$name — cloning"; \
			git clone --quiet "$$url" "$$dir" || exit 1; \
		fi; \
		if [ -n "$$(git -C "$$dir" status --porcelain)" ]; then \
			echo "$$name: working tree is dirty — refusing to move it" >&2; exit 1; fi; \
		cur=$$(git -C "$$dir" rev-parse HEAD); \
		want=$$(git -C "$$dir" rev-parse --verify --quiet "$$ref^{commit}" 2>/dev/null); \
		if [ -z "$$want" ]; then \
			git -C "$$dir" fetch --quiet --tags origin || exit 1; \
			want=$$(git -C "$$dir" rev-parse --verify --quiet "$$ref^{commit}" 2>/dev/null); \
		fi; \
		if [ -z "$$want" ]; then echo "$$name: pinned ref $$ref not found on origin" >&2; exit 1; fi; \
		if [ "$$cur" = "$$want" ]; then echo "# $$name — already at $$ref"; else \
			git -C "$$dir" checkout --quiet --detach "$$want" || exit 1; \
			echo "# $$name — $$(echo $$cur | cut -c1-9) -> $$(echo $$want | cut -c1-9)"; \
		fi; \
	done < $(REPOS_FILE); \
	if [ "$$n" -eq 0 ]; then \
		echo "$(REPOS_FILE) declares no targets — refusing to report a successful sync" >&2; \
		exit 2; fi

## sync-status: show how far each pinned target has fallen behind its remote
# Read-only: fetches, reports, and moves nothing. This is what to run before
# deciding to advance a pin. Same zero-target refusal as sync, for the same
# reason: "0 targets behind" and "nothing declared" must not read alike.
sync-status:
	@if [ ! -r "$(REPOS_FILE)" ]; then \
		echo "$(REPOS_FILE): no manifest, so no targets to report on" >&2; \
		echo "  declare one per line as '<name> <url> <pinned-ref>'" >&2; \
		exit 2; fi
	@n=0; while IFS=' ' read -r name url ref; do \
		case "$$name" in ""|"#"*) continue ;; esac; \
		n=$$((n + 1)); \
		dir="$(PROJECTS_DIR)/$$name"; \
		if [ ! -d "$$dir/.git" ]; then echo "$$name: not cloned — run 'make sync'"; continue; fi; \
		git -C "$$dir" fetch --quiet --tags origin || exit 1; \
		head=$$(git -C "$$dir" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || echo origin/develop); \
		tip=$$(git -C "$$dir" rev-parse --verify --quiet "$$head" 2>/dev/null); \
		if [ -z "$$tip" ]; then echo "$$name: cannot resolve $$head"; continue; fi; \
		behind=$$(git -C "$$dir" rev-list --count "$$ref..$$tip" 2>/dev/null || echo "?"); \
		echo "$$name: pinned $$(echo $$ref | cut -c1-9) — $$behind commit(s) behind $$head ($$(echo $$tip | cut -c1-9))"; \
		if [ "$$behind" != "0" ]; then \
			git -C "$$dir" log --oneline --no-decorate -5 "$$ref..$$tip" | sed 's/^/    /'; fi; \
	done < $(REPOS_FILE); \
	if [ "$$n" -eq 0 ]; then \
		echo "$(REPOS_FILE) declares no targets — nothing to report on" >&2; \
		exit 2; fi

##@ Housekeeping

## clean: remove the built binary and drop the Go test cache
clean:
	rm -f formwork
	go clean -testcache
