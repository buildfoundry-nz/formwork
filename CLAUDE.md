@AGENTS.md

## Working in this repository

`AGENTS.md` is the operating manual and everything in it applies. This file adds
only what is specific to running an agent here.

### Before you change anything

- **Read the spec for the subsystem you are touching** (`docs/specs/`). The
  design is written down, and a change that contradicts it needs the spec
  updated in the same commit with the reasoning — not a comment explaining why
  the code disagrees.
- **Reproduce the defect before writing the fix.** Filed reproductions go stale:
  more than once here a bug report no longer reproduced against current `main`,
  and the honest outcome was to say so rather than to write a test for a defect
  that had already been fixed. Probing first is cheaper than a review round.

### The two mistakes that recur

1. **A green gate you did not force to run.** `make verify` was green before you
   started, so it proves nothing about your change. Name the thing that was not
   true before — the new test by name, failing on the parent commit and passing
   on yours; the mutation you applied and the specific test that reddened.

2. **A confident wrong comment.** Comments here are load-bearing, and the next
   reader trusts them — usually an agent with no memory of why they were
   written. If a comment argues for a design, every claim in it is checkable. Do
   not assert anything about a file you did not open in this change.

### Verifying your work

Run `make verify` and **read the exit code yourself**:

```sh
make verify > /tmp/v.log 2>&1; echo "exit: $?"
```

Never through a pipe, never chained with `&&` off an `echo`. See the
non-negotiable in `AGENTS.md`; it has been got wrong repeatedly, in more than
one spelling.

### Scope discipline

Deliver what was asked. If you find a second defect while fixing the first,
file it or note it — do not fold it in. A response larger than the change that
prompted it is the signal to stop and split, and it has generated more findings
than it closed every time it was ignored here.
