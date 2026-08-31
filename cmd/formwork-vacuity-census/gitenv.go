package main

import (
	"os/exec"
	"strings"
)

// gitCmd spawns git with the repository LOCATION variables removed from the
// child environment. Every git child in this tool goes through it.
//
// The census always knows the repository it means — callers pass an explicit
// root and every spawn carries it as `-C root`. `-C` does NOT override an
// inherited GIT_DIR: git resolves the repository from the environment before
// it considers -C, so without this scrub the environment can answer for a
// different repository than the one the caller named (#16046).
//
// The census reads merge-bases, staged diffs and `git archive` of a rev. Each
// of those returns a plausible answer for the WRONG tree under an inherited
// GIT_DIR rather than failing, which is the direction that does damage: a
// vacuity verdict computed against another repository still looks like a
// verdict. GIT_INDEX_FILE is scrubbed for the same reason one level down.
func gitCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = scrubGitEnv(cmd)
	return cmd
}

// scrubGitEnv returns cmd's environment with the git LOCATION variables
// removed. Assign it back as cmd.Env before spawning.
//
// It reads cmd.Environ() rather than os.Environ(): an os.Environ() walk in a
// non-test file is banned outright (env-gates-no-app-env-literal, #10581),
// because a walk reads every variable at once and a name can then hide behind
// concatenation where no literal-anchored arm can reach it. cmd.Environ() is
// also the more accurate source — it is the environment this command would
// actually run with, and it is the shape persistlib's gitOut already uses.//
// The set is the SIX keys api-factory/internal/lockdowntests/
// git_command_seam_test.go declares as gitEnvBlockedKeys (#13973) — the
// authoritative list. Do not restate it from memory here or anywhere else: a
// four-key copy of it shipped across this campaign because each author copied
// the previous author's list rather than the source, and the two
// object-directory keys redirect the object store on their own, with neither
// GIT_DIR nor GIT_WORK_TREE set.

func scrubGitEnv(cmd *exec.Cmd) []string {
	src := cmd.Environ()
	env := make([]string, 0, len(src))
	for _, kv := range src {
		switch {
		case strings.HasPrefix(kv, "GIT_DIR="),
			strings.HasPrefix(kv, "GIT_WORK_TREE="),
			strings.HasPrefix(kv, "GIT_COMMON_DIR="),
			strings.HasPrefix(kv, "GIT_INDEX_FILE="),
			strings.HasPrefix(kv, "GIT_OBJECT_DIRECTORY="),
			strings.HasPrefix(kv, "GIT_ALTERNATE_OBJECT_DIRECTORIES="):
			continue
		}
		env = append(env, kv)
	}
	return env
}
