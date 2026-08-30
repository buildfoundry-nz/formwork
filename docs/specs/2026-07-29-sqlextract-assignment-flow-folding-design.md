# `sqlextract` assignment-flow folding (`FromGoReassembled`)

Status: implemented; revised after a four-lens adversarial review and again for
#42 (see §10 — suppression abandoned, emission widened).
Issue: [#36](https://github.com/buildfoundry-nz/formwork/issues/36).
Relates to [#12](https://github.com/buildfoundry-nz/formwork/issues/12)
(phase 3d — closed by PR #33, which left this case open by design).
Design spec cross-ref:
`docs/specs/2026-07-28-sql-parse-tree-rules-design.md` §1
("Status after phase 3d").

## 1. Problem

`sql/locking-select-order` exists to catch a deadlock hazard: a locking
`SELECT` that grabs many sibling rows without a deterministic `ORDER BY`. In a
`.go` file it can only see a query where the whole statement is **one
expression** — a string literal, a `+`-chain, or a `fmt.Sprint{,f,ln}` call.

A query assembled **across two statements** is invisible to it:

```go
q := `SELECT ... FROM palletra.projects WHERE id = ANY($1) ...`  // no lock here
if forUpdate {
    q += "\n\t\t ORDER BY id FOR UPDATE"                          // lock in a 2nd statement
}
```

`sqlextract` folds *expressions*, not *assignment flow*, so these are two
separate candidates: the first carries no `LockingClause`, the second is not a
parseable statement and is dropped. Neither reaches `lockingSelects`, and the
rule reports nothing — however unsafe the real query is.

**A clean run is then read as "no deadlock risk" when it actually means "not
analyzed".** That is worse than having no rule, because the rule is believed.
Reproduced against the validating target: the shape above is live at
`freightworks/services/core-api/internal/deskallocation/commit.go:271-278`
(`loadAssignmentStates`, called by `LoadAssignmentStatesForUpdate`). Copy that
file out and delete its `ORDER BY id` to manufacture a genuine violation, and
the rule still produces **no finding** — the outcome the new
`internal/rules/sqlparse/locking_test.go` case asserts as RED (§7). This is the
straight-line/`if`-branch slice of a broader silent-give-up class; `strings.Builder`,
loop/`switch`-built, and mixed-independent-flag composition remain out of reach
and are disclosed as coverage limits (§9), not silently "handled". Closure
composition was in that list until #72: an **immediately-invoked** literal is
now folded inline **when its body is fully modellable** (§4.2) — a body
containing so much as a plain function call is not, and is left exactly as
unanalyzed as before — and what any other closure appends, or what a
disqualified IIFE's own body appends, is a disclosed miss or false positive
(§4.2, §9).

## 2. Scope

**In:** assignment-flow folding within a block — `x := …` then `x += …`,
including `x += …` inside an `if`/`else` branch — in `FromGoReassembled` only.
Additive, exactly as `FromGoReassembled` was itself added alongside `FromGo`.

**Out:**

- `FromGo` — must stay byte-identical in effect. `sqltext` and 19+ external
  consumers depend on its candidate set.
- `strings.Builder` composition — same root cause, larger job; file separately.
- Loop/`switch`/`select`-built queries — untracked (§4.1), under-reported (§9).
- Closure-captured mutation, **except** an immediately-invoked literal whose
  body is fully modellable, which is folded inline (§4.2, #72) — one that is
  not is left exactly as unmodelled as any other closure. Modelling what a
  *named* closure appends needs dataflow this pass declines; disclosed in §9.
- Cross-function or cross-file flow; anything needing `go/types`. The extractor
  is deliberately parse-only so it works on a tree that does not compile.

## 3. Design decision: bounded per-path folding (all-or-nothing over optionals)

When a query is folded across a conditional, the variable has more than one
possible final value. Three emission strategies were weighed against two failure
modes a **lockdown** rule cannot tolerate: a false positive on provably-safe
code (it forces a `formwork:allow` on correct code, which this project's
no-escape-hatches standard forbids) and a *silent* false negative (a clean run
that reads as safe — the #36 bug itself).

- **A — flatten all appends unconditionally** into one candidate. Simple, passes
  the deskallocation case, but hides a hazardous path behind a compliant fragment
  from another branch:

  ```go
  q := `SELECT ... FROM t WHERE ...`
  if withOrder { q += " ORDER BY id" }
  q += " FOR UPDATE"                    // always locks
  // A -> SELECT ... ORDER BY id FOR UPDATE -> compliant -> PASSES,
  // but the !withOrder path (lock, no order) is a real hazard. MISSED.
  ```

- **B — full per-path enumeration** (union every branch outcome). Catches A's
  missed path, but treats independent conditionals as decorrelated and so
  *manufactures infeasible paths*, firing on provably-safe code:

  ```go
  if forUpdate { sql += " ORDER BY id" }   // when we lock, we DO order
  if forUpdate { sql += " FOR UPDATE" }
  // B emits the phantom `base + FOR UPDATE` (lock, no order) -> FIRES on a
  // query that is always safe. Only "fix" is formwork:allow on correct code.
  ```

  B also grows `2^k` states for `k` optional appends (filter/search builders
  routinely have 6+), forcing a cap whose overflow would silently drop the
  variable — recreating #36.

- **C — bounded per-path, all-or-nothing over optionals (CHOSEN).** Emit only
  two worlds per variable: **every optional append taken** (`full`) and **no
  optional append taken** (`base`). Never emit a value that takes one optional
  while skipping another, so no infeasible-partial FP (kills B's decorrelation
  fire) and no `2^k` blow-up (kills the silent cap). It still fires on the
  deskallocation shape both ways **and** on A's missed path (the unconditional
  `FOR UPDATE` survives into `base` while the optional `ORDER BY` does not):

  ```go
  q := base
  if withOrder { q += " ORDER BY id" }
  q += " FOR UPDATE"
  // full = base + ORDER BY id + FOR UPDATE  -> compliant
  // base = base + FOR UPDATE                -> lock, no order -> FIRES
  ```

  The price is one narrow, *disclosed* miss (§9): a hazard reachable only by a
  *mixed* combination of two genuinely-independent optional flags — optional
  lock on, optional order off. `full` (both on) and `base` (both off) are both
  safe there, so C stays silent. That is an honest coverage limit, treated like
  `strings.Builder`, not a silent give-up dressed as a pass.

  `full` assumes the optional conditions can co-occur. When they are mutually
  exclusive, `full` is unreachable — but it carries *more* appends (more likely
  to include an `ORDER BY`), so it errs toward *compliant*, the safe direction;
  it never invents a FP.

  The mirror case is **not** self-correcting and was a real FP (#42): `base`
  assumes the optional conditions can all be *false at once*. When two optional
  appends are guarded by **complementary** conditions (`if a` / `if !a`), at
  least one always fires, so the "no optional taken" world is unreachable — and
  `base` carries *fewer* appends (more likely to *miss* an `ORDER BY`), so it
  errs toward *fires*, the unsafe direction, inventing a FP on code ordered on
  every real path.

  **Both extremes are infeasible under a complementary pair, and deleting one is
  not the fix.** `a` and `!a` cannot both hold either, so `full` is unreachable
  for the mirror reason. Suppressing `base` and keeping `full` alone (the first
  attempt at #42) traded the FP for false *negatives*, the worse direction in a
  lockdown gate: with branches appending **different** clauses (`if a { += ORDER
  BY }` / `if !a { += LIMIT 10 }`) the real `!a` path is an unordered lock that
  no remaining world models, and an **independent** optional alongside the pair
  (`if wantOrder { += ORDER BY }`) has its own reachable false world erased,
  because suppression was applied to the whole variable rather than to the
  appends the pair covers.

  What is reachable then is **one branch per complementary name**, so when two
  optional appends are guarded by a condition and its negation the fold emits
  those two branch worlds as well — each rendered *minimally* (every append the
  branch does not force is off) and *maximally* (they are on), which keeps the
  all-or-nothing bound over the appends the pair does not reach. That closes §9's
  miss for an order/lock pair split across one flag's opposite polarities (`if !x
  { += ORDER BY }` / `if x { += FOR UPDATE }`): the x=true world is a lock with no
  order and now fires.

  **`full` and `base` are always emitted, and `base` is never suppressed.** This
  is the resolution of #42, and it does not fix #42. Four review rounds were
  spent trying to remove `base` under a complementary pair, each round narrowing
  the conditions under which the pair counted as *proven*:

  1. suppress whenever a condition and its negation appear (7 defects);
  2. suppress unless a write intervened, tracked by a per-value **epoch**
     (10 defects);
  3. epochs plus correlation facts, closure positions and nested pairs
     (10 defects);
  4. one pair, a whole-block purity scan, and widening when unprovable
     (10 defects).

  Every round the reviewers found more ways to write a guard that the analysis
  did not see — a helper call handed an options struct, a method value, a pointer
  in a composite literal, a reverse alias, a multi-value binding, an embedded
  field, an alias created inside a closure — and each miss **wrongly proved** a
  pair, deleted a reachable world, and silenced a real unordered-locking `SELECT`.
  The lesson is structural, not a tally of bugs: removing `base` requires proving
  the two `if`s read **one value**, which requires alias analysis, which requires
  the types this pass deliberately does not have (§2). Enumerating write shapes
  is unbounded; the reviewers will always be ahead.

  And a *correct* proof would still not be enough. Complementarity says the
  variable's **final** value never lacks both appends. It says nothing about the
  value in between, so a query observed between the two branches —

  ```go
  q += " FOR UPDATE"
  if a  { q += " ORDER BY id" }
  run(q)                          // observes base: a locking SELECT, no ORDER BY
  if !a { q += " ORDER BY name" }
  ```

  — is `base` on a real path, with no write anywhere for an analysis to find.

  So the model keeps the half that is sound. **Emission only ever grows:** the
  bounded pair unconditionally, plus the branch worlds where a complementary
  guard exists. That makes the emitted set a strict **superset** of the pre-#42
  model, so no shape it reported can go silent — by construction, not by
  analysis. The cost is #42 itself: `base` may be a world no path produces, a
  false positive on code ordered on every real path. That is a *visible* finding
  a `formwork:allow` marker clears, and `formwork lint` keeps the suppression in
  view; the alternative was silence on a deadlock hazard, which nothing surfaces.
  A lockdown gate takes the first trade. #42 stays open, retargeted at whatever
  can carry the proof — a typed pass, or none.

  Two collection rules survive, both narrowing what counts as a *pair* rather
  than what counts as *proof*:

  - the two appends must share an **identical guard prefix** and differ only in
    the polarity of their last guard. An append fires on the conjunction of every
    guard enclosing it, so `useTx && a` against `useTx && !a` is a pair for
    exactly the reason `a` against `!a` is — whenever the prefix holds, one of
    them fires. Differing prefixes are *not* paired: an append under
    `if a { if b { … } }` says nothing about an `if !b` elsewhere, because
    `a = false` skips both and fixing `b` forces neither;
  - only a **stored value** read directly: an ident or a chain of field
    selections off one (`a`, `o.Ordered`), optionally negated, in an `if` with no
    Init. A call is not one (`opts.Order()` can return two different values), a
    compound condition is not tracked, and an `if`-Init may *bind* the name it
    tests. Each exclusion simply leaves the variable on the bounded pair.

  **Every pair is enumerated, separately, up to three.** A world fixes exactly
  *one* truth value; every other pair's appends stay open under the
  all-or-nothing bound. That is what keeps the cross-product out — no world ever
  asserts two flags at once, so the reachable-only-if-independent problem
  (`b := a`) is never posed. Enumerating only the first pair was the earlier
  rule and it failed in the silent direction: a variable with two complementary
  flags has *more* hazard surface than one with a single flag and was given
  *less* analysis, because the second pair collapsed it to `full`/`base` alone.

  The cap of three bounds emission at `2 + 4×N ≤ 14` texts per variable. It is a
  **cost** bound — every text is a `pg_query` parse on wazero, and those dominate
  the rule's runtime — and it degrades by *truncating* the sorted candidate list,
  never by collapsing: past the cap the variable still gets `full`, `base` and
  the earlier pairs' worlds, strictly more than the one-pair rule emitted.

  Enumerating pairs separately does not make correlated flags understood, and
  costs a second disclosed false positive: fixing one flag while another is a
  copy of it (`b := a`) yields a minimal world that turns off appends the copy
  forces on, so a query ordered on every real path can fire. Same defect as #42,
  same trade — a visible finding a marker clears, over silence.

  An append the enumeration does not reach is still governed by the
  all-or-nothing bound (off in the minimal world, on in the maximal one), but
  never in a world that fixes one of its enclosing conditions *against* it: the
  nested `if b { += FOR UPDATE }` inside `if a { … }` cannot appear in the
  `a = false` world at all.

## 4. Mechanism

A new, self-contained block-walk pass — `foldAssignments` — runs **alongside**
the existing expression walk inside `FromGoReassembled`. The expression walk is
left byte-identical; folding only *adds* candidates.

### 4.1 State machine (per block scope)

Walk statements linearly. For each **single-ident local variable** whose value
is string-composed, track two single-string accumulators, `full` (every append
applied) and `base` (only unconditional appends applied):

- `q := <expr>` — **seed** both to `reassemble(expr)`, but only when the RHS
  reassembles to *real literal text* (a literal, a `+`-chain, or a
  `fmt.Sprint{,f,ln}` call). `q := buildQuery()` seeds nothing and is not
  tracked. `foldAssignments` MUST seed via **the same `reassemble`** the
  expression walk uses — that shared primitive is *why* the additivity invariant
  (§4.3) holds, and a future refactor must not silently split them.
- **Unconditional** `q += <expr>` — append `reassembleOperand(expr)` to **both**
  `full` and `base`.
- `q = <expr>` — reset both to `reassemble(expr)` for a string RHS; otherwise
  untrack `q`. A `:=`/`=` to a tracked name **inside** a branch (a possible
  shadow) untracks it — parse-only cannot tell a shadow from a reassignment.
- A non-literal RHS contributes the synthetic `fw_expr` placeholder, exactly as
  today (see §4.4 for the honest limits of that).
- **Optional** `q += <expr>` (inside an `if` branch with **no** `else`) —
  append to `full` only; `base` skips it. This is the all-or-nothing rule: an
  optional append is in `full` (all optionals taken) and absent from `base` (no
  optional taken), so we never emit a value that takes one optional while
  skipping another (the decorrelation false positive C exists to avoid).
- Any assignment to a tracked variable inside a construct this pass does **not**
  model — `for`, `range`, `switch`, `select`, **or an `if`/`else` whose branches
  both append** (a mandatory choice, deliberately *not* forked — see §4.2) —
  **untrack** the variable. Never emit a value that cannot be proven a real
  prefix. This is sound (untracking only drops the variable → a later `+=` adds
  to nothing → a MISS, never a wrong emit). The guarantee is about a write the
  walk **sees and refuses**; a write it does not see at all, while the variable
  stays tracked, is the opposite — the fold then emits a value assembled from
  the writes it happened to see, a wrong emit. That was #72, #73 and #74 alike.
- The **`for … = range` clause** is the one write the walk sees and does **not**
  always refuse, and #314 is why. Its target is written only if the loop runs, so
  over a map, slice, channel or iterator function the zero-iteration path is a
  real one on which the variable survives — the world built from the appends
  around the loop is produced, and untracking would delete a true positive (#72's
  third comment puts that shape out of scope for exactly this reason). It is
  untracked where the source **provably** iterates and only there, decided from
  syntax alone: a composite literal with an element, an array literal whose
  length is positive however few elements are written, and a name the scope
  proves an ARRAY — declared, parameter, named result, or assigned an array
  literal — because an array's length is part of its type and no later assignment
  can shorten it. A `:=` range clause binds a new name and writes nothing here.
  Before #314 no `range` clause was seen at all: `untrackAssigned` matched
  `*ast.AssignStmt`, and a clause write lives in `RangeStmt.Key`/`Value`, so this
  bullet's `range` promised what only a range BODY delivered.
- A **forward `goto`** skips the statements between it and its label, so those
  appends are **optional** (opaque — the skip condition is not carried to the
  label) and the jumped-over world is emitted beside the fall-through one. This
  is deliberately additive: `full` still applies every append, so untracking —
  which the bug report proposed — would instead have deleted the fall-through
  world, which is real. A **backward** `goto` makes its label a loop head; that
  is unmodelled and the labelled statement is untracked, as `for` is. A
  forward-jump label is otherwise folded through, where reaching the default arm
  used to untrack a labelled `q += …` and every append after it.
- A **write through a taken address** is a write this walk cannot see, so the
  name it could reach is **untracked and nothing is emitted for it**. `*p op= …`
  is NOT read back as `q op= …` — this bullet said it was, for two days after
  #74 landed, and it never has been (#312). Nothing here resolves a dereference
  to the variable behind it, and adding that would open a new EMISSION path
  where every mechanism in this section only ever removes one; `locking.go`'s
  shipped COVERAGE LIMIT is the accurate text and says UNTRACKED, NOT RESOLVED.
  Two analyses reach the same refusal from different directions.
  `aliasUnsafe` (`unseenwrite.go`) is the DEREFERENCE-WRITE one: a block
  containing any `*x =`/`*x +=` untracks every name whose address it takes,
  deliberately over-approximate, because proving `*p` aliases `q` needs pointer
  analysis §2 declines and an unrelated pointer write is cheaper to over-refuse
  than to model. Taking an address only to READ through (`len(*p)`) is not a
  write, and leaves every append visible and the query analyzed.
  `escapedNames` (`escape.go`) is the ESCAPE one: every `&q` handed to a helper,
  stored, sent, returned, or put in a composite literal untracks `q`, because
  what the far side does with it is not in this file. A `p := &q` or
  `var p = &q` bound at the **top level of the block** where every other mention
  of `p` in the body is a dereference is exempt from THAT untrack and left to
  the analysis above — this block's own text decides what `p` names. A binding
  inside a nested scope resolves nothing: the exemption is keyed by bare name
  with no scope stack, so an inner `p := &other` must not decide what an
  enclosing `*p` writes.
  An escape untracks **only where it provably runs**, and that is two
  independent conditions: the enclosing branch context and the position inside
  the statement. The shipped set is exact and worth reading as a list rather
  than as a principle — an assignment's targets and its values, an expression
  statement, a channel send's operands, a `var` initialiser, an `if`'s Init and
  its condition, and the statement under a label. Everything else contributes
  nothing: BOTH arms of an `if` (not merely an `if` without an `else`), a
  `for`/`range` body, a `switch` case, a `select` clause, a `defer`/`go` call, a
  `return` expression, any nested block, and any function-literal body. Each has
  its own reason — a loop body runs zero times on an empty slice, a `switch`
  case may match nothing, a `select` clause is one of several, a `defer`/`go`
  call has not run when the value reaches the driver, a `return` expression
  evaluates after every append, and a nested block may SHADOW the name with a
  `q := …` this classifier cannot tell from the outer one. Under a branch the
  not-taken path is real, so the world assembled without the callee's writes is
  real and a finding on it is a true positive; deleting those is the measured
  mistake of §10's first design. What the helper actually writes is still out of
  reach (§2), so that case is an honest non-analysis rather than a fabrication
  (§9 item 4).
- An **immediately-invoked** function literal (`func(){ … }()`) whose body is
  fully modellable is folded inline rather than untracked — it provably runs,
  once, here (§4.2). One whose body is not is neither of those: it is left
  opaque **and** stays tracked, reproducing #72 rather than landing safely on
  either side of that binary (§4.2, §9). Every LHS identifier is read through
  `ast.Unparen`: `(q) += …` is valid Go and is an `*ast.ParenExpr`, and matching
  a bare `*ast.Ident` without untracking on the miss was a fabrication with no
  closure involved (§4.2).

Only single-ident LHS is tracked. Selector/index targets (`x.q +=`, `m[k] +=`)
and multi-value assignments are ignored.

### 4.2 Scope roots, nesting, and emit-timing

Each `*ast.FuncDecl` body and each `*ast.FuncLit` body is an independent scope
root. `if`/`else` bodies are recursed *within* a root's walk.

**An immediately-invoked function literal — `func(){ … }()`, no parameters, no
results, body fully modellable — is folded INLINE, in source order, in the
enclosing block.** It is the only closure shape that provably runs, exactly
once, at the point it is written, so its appends interleave with the
surrounding ones and the reassembled value is the real one rather than a guess
about it. `foldStmts` recurses into its body under the *same* guard context, so
an `if` inside an IIFE keeps its optional/guard semantics with no extra
machinery. Parameters are excluded because they can shadow the name the body
writes; a *named* result is excluded for the same reason — a naked `return`
assigns through the shadowed name — but an unnamed result cannot shadow this
way and is excluded anyway, rather than carving out a narrower rule for the one
shape that cannot fabricate. A body is inlined
only when every statement in it, recursively, is an admitted assignment, an `if`
with neither an `Init` nor an `Else`, or a further recognised IIFE; anything else
— **a plain function call** (the commonest shape an IIFE body actually has), a
`return`, a loop, a switch, a bare block, a declaration — leaves the whole
literal opaque, because once a body is inlined its statements share the
*enclosing* scope and a construct this pass does not model can reach a variable
no path inside the closure touches.

**Inside an inlined body no statement may untrack an outer variable, be folded
into one it does not really write, or leave the value unparseable where base
left it readable.** Every statement there satisfies exactly one of three arms: it
provably cannot write an outer variable, and is **skipped**, neither tracked nor
untracked; it is an append this pass can render as real text, and is **folded**;
or neither holds, and it **disqualifies the whole body**, leaving the literal
opaque and reproducing pre-#72 behaviour byte for byte. Untracking is never one
of them. It reads as the conservative move and is the opposite:
`foldStmts` and `foldAssign` delete by *bare name* with no scope stack, so an
untrack reached from inside an inlined body deletes the enclosing scope's
variable, and every later append to it with it. A `:=` is the case that makes
this concrete — it binds anew in a block of the closure, so the outer variable
of that name is *provably never written* — and untracking it deleted a variable
nobody wrote: measured at the gate, five shapes went from base's one finding on
an unordered locking SELECT to none at all. It is skipped, however many names it
binds and however deep in the body it sits. So is an `=` that names no variable
(`_ = q`, `o.f = q`). An `=` with a *named* target might write the outer
variable — `func(){ if b { q = "z" } }()` really does when `b` — and parse-only
cannot tell that from a shadow, so it disqualifies the body; so does any other
compound assignment (`-=`, `*=`, …), which likewise writes a variable that
already exists.

Skipping a `:=` opens one hazard of its own, and closing it is part of the same
rule. Because the accumulators are keyed by bare name, a later `q += …` in the
same body would be folded into the **outer** `q` although it appends to the
closure-local one — fabricating a value no path produces, and an *ordered* one
in the shape that matters, which reads as safe and silences the outer lock. A
body that both **binds** a name with `:=` and **appends** to it with `+=` is
therefore not inlined at all. That check is order-blind and scope-blind by
choice, and refusing is **not** merely a precision cost the way the narrowings
below are: whenever the wrongly-disqualified body also contains a real,
unconditional append a correct scope analysis would keep, refusing the whole
body drops it too, reproducing the *exact* #72 pair. Measured on the
order-blind sub-case (an append before the shadowing `:=`, which Go scopes
from the statement after it, not the whole block): an `ORDER BY` appended
there fabricates an unordered value where the real one is ordered (**1
finding** at the gate); a lock appended there drops the query's only lock
entirely (**0 findings** on a genuinely unordered locking SELECT). The
scope-blind sub-case (an append after an `if`-block whose local binding never
reaches it) reproduces both directions identically. Deciding it properly needs
the scope stack this pass does not have, and a wrong scope proof re-opens
exactly that fabrication.

The admitted `+=` is narrowed twice more. Both land on the **same floor as the
check above, and that floor is not safe**: refusing disqualifies the whole body,
which leaves the literal opaque while the variable stays tracked — the exact #72
pair (§9's first false positive). What makes refusing right here is not that it
is free, but that *admitting* is worse: the append would fold to a value the
parser rejects and the candidate would be dropped in silence, so the choice is
between two silences and refusing at least leaves base's coarser, parseable
candidate reaching the gate. The residual coverage cost is stated exactly below;
"costs precision only" would be the same overclaim this section already corrects
for the shadow check. It must have
**one target and one value**: `go/parser` accepts `a, q += "1", "2"` — a type
error, not a parse error — and that reaches the multi-target untrack, which
deletes the outer `q`. This pass never type-checks its input, so "it would not
compile" is not a reason to let an untrack through. And what its RHS *renders
to* must carry real literal text **before its first `fw_expr` placeholder** —
not merely somewhere in the rendering. The fold concatenates an append onto the
accumulated value with no separator, so a placeholder standing first is glued
to that value's last token: `…WHERE s = 'x'` + `fw_expr` is a token no query
contains, and the parse has already failed to the *left* of the placeholder —
nothing after it, inside the same append or not, can rescue that. This refuses
an append with no literal text at all (`q += buildClause()`, `q += clause`,
`q += parts[0]`) and, for the same reason, one whose only literal text sits
*after* the placeholder (`q += clause + " ASC"`, `q += fmt.Sprintf("%s ORDER BY
id", clause)`): each renders to a value the fold cannot make parseable, so the
candidate is dropped in silence — where base, having left the literal opaque,
emitted a coarser value that *did* parse and reported the lock. A **mixed**
append with literal text *before* the placeholder (`" ORDER BY " + col`) is
different: its placeholder lands in a column position rather than glued to the
preceding token, so it stays admitted, and it is how a lock or an order added
inside a closure is seen at all.

Refusing a body restores base exactly, unconditionally; "costs no coverage"
does not follow from that as generally as it reads. The check above is a pure
function of the *operand's own* rendered text — it never looks at what that
text is about to be glued onto — so it cannot tell a placeholder landing after
a completed literal (`'x'fw_expr`, unparseable) from the same placeholder
landing right after a bare comparison operator (`s = fw_expr`, which parses).
Before this check existed, a bare `q += clause` glued onto a seed ending
`s = ` folded, parsed, and fired the gate; the shipped check refuses that shape
too, uniformly, because telling the two apart needs the accumulated value this
predicate never sees. The trade is real: a strip of coverage given up so the
check can stay a pure function of the closure's own AST.

This is a *bound*, not a decision procedure. Whether a placeholder-bearing value
parses depends on where in the query it lands — `WHERE s = 'x' fw_expr` is a
syntax error while `ORDER BY fw_expr` is a column reference — and that is not
knowable without parsing, which this pass does not do. Mixed appends whose
placeholder lands in a value position are therefore still dropped where base
reported (§9); closing that completely would mean refusing every
placeholder-bearing append, which would take the common `" ORDER BY " + col`
shape with it.

An IIFE body is consequently walked twice: inline here, and again as its own
scope root. The pre-#72 text of this section forbade that ("no statement is
processed twice"), but the hazard it was guarding against was double *emission*,
and that does not arise — the two walks track disjoint variables. The inline walk
sees the enclosing block's variables and folds appends into them; the root walk
starts from an empty scope, so a name the IIFE merely appends to is untracked
there and contributes nothing, while a query the IIFE seeds *and* appends itself
belongs to the root walk alone — which is the bind-and-append rule above seen
from the other side, not a second mechanism. Candidate count is unchanged.

**Every other closure stays opaque, and the enclosing walk keeps folding.** This
reverses both what this section claimed before #72 — that a closure capturing and
mutating an outer variable is "simply unmodeled", which was false, since the
variable stayed *tracked* while its appends vanished — and the first attempted
fix, which refused such a variable outright. Both were wrong, in opposite
directions, and one measurement settles it:

> for a closure that is *conditionally called*, *never called*, or *created after
> the value is used*, the value assembled from the appends OUTSIDE the closure is
> a value the program really produces — the not-called path. Emitting it is
> correct and a finding on it is a **true positive**.

Refusal deletes those findings. Measured across fifteen shapes it removed ten
findings, of which eight were true positives against the one false positive it
targeted, and it manufactured a new silence besides: keyed on the bare name
across a whole function body, a closure writing `q` in one branch killed an
unrelated `q` in a sibling branch. Refusal also *cannot* fix the direction that
matters, because the gate fires per emitted candidate — subtracting candidates
can only ever subtract findings, never catch a hazard that was missed. Folding
the IIFE inline catches it (§9, and `foldworlds.go`'s "emission only ever
grows").

**A named closure this pass can SEE being called untracks the variable it
appends to** — `add := func(){ q += … }` then `add()` emits nothing, rather than
the world without `add`'s append. This paragraph said the opposite ("one honest
residue remains … the value emitted for it is fabricated") for a day after #72
closed it and for two more after #337 widened it (#312). The narrowing is the
CALL, not the closure: one that is never called, called under a branch, or
created after the value is used leaves the outside-appends world real, and
untracking those deletes true positives — the measurement three paragraphs up.
Which spellings the pass can see is `invoked.go`'s subject and #337's, because
answering it in one place is what stopped the next spelling being open when the
next review looked: `var add = func(){ … }`, a call inside a bare block, a call
through an alias (`g := add; g()`), a call whose result is assigned or sent, and
a call inside a literal that itself provably runs are one unconditional call
written six ways, and each of them fabricated while only `add()` was matched. A
seventh is not reached and is not going to be — `run(add)`, the closure's name
handed to a call rather than called — and the paragraph below is why.

**What that widening does NOT reach, and will not**: the closure's NAME handed
to a call — `run(add)`, against `func run(f func()) { f() }`. `run` invokes it,
so the ORDER BY runs on every real path and the outside-appends world is one no
path produces; the variable stays tracked and the gate fires. **#337 — DECIDED:**
it is kept rather than closed, and the measurement is the reason. To a pass that
reads one file and never resolves a callee, that program is the SAME TEXT as
`register(add)` against `func register(f func()) {}` — `run(add)` and
`register(add)` both measure **1 finding** at `unique_key_columns: [id]` — and
there nothing calls the closure, `… FOR UPDATE` really is the value, and the
finding is the deadlock hazard this rule exists for. Untracking on the escape
deletes the second to delete the first, and it splits `run(add)` from
`run(func(){ … })`, which folds today and is pinned green as §10's rejected
design. Disclosed as the fourth false positive in §9 and as
`SHAPE closure-name-escape FIRES` in `locking.go`, whose fixture this
paragraph's spelling is read out of by
`TestFoldSpecDoesNotClaimTheCalledClosureClassIsClosed`.

**Writes are read through parentheses.** `(q) += " ORDER BY id"` is valid Go,
survives `gofmt`, and puts an `*ast.ParenExpr` on the LHS. `foldAssign` matched a
bare `*ast.Ident` and *returned without untracking* when that failed, so the
variable stayed tracked and the append vanished — the same fabrication as #72
with no closure involved at all. Every LHS identifier goes through `ast.Unparen`,
as `foldGuard` and `guardPath` two functions away already did. That covers plain
block level and nothing else: this paragraph also claimed `for (q) = range m`,
and `foldAssign` never could reach it, because a `RangeStmt` is not an
`AssignStmt` and arrives at no LHS of any kind. `unmodelledWrites` reads that
clause's `Key`/`Value` through the same `ast.Unparen`, which is where the
spelling is actually covered (#314).

`if`/`else` where **both** branches append is a *mandatory* append with a text
choice. It is deliberately **not** forked: forking would make the accumulators
set-valued and introduce `2^m` growth for a shape that is vanishingly rare in
real locking-query builders. Instead the variable is **untracked** (§4.1) — a
sound, disclosed miss (§9). The accumulators therefore stay single strings and
a variable yields **at most two** candidates (`full` and `base`), with no cap
and no set bookkeeping.

**Emit-timing is block-end**, not incremental: `foldAssignments` emits a
variable's `full`/`base` values once, when its scope-root walk completes.
Incremental emission would fire on non-final intermediates (`q += " FOR UPDATE"`
before a later `q += " ORDER BY id"`), a false positive. The trade — a query
*used* mid-block before a further append is analyzed only at its final value — is
a disclosed under-report (§9), consistent with the sound-not-complete stance.

The value set is emitted in **deterministic (insertion) order**, not raw map
order, so candidate order is stable (`sqlextract` owes deterministic output).

### 4.3 Emission rule and the additivity invariant

`foldAssignments` emits **only values that contain at least one appended
fragment** — the joined values the expression walk does not already produce. The
pure-seed value is already emitted by the existing expression walk (it is the
`:=` RHS literal, always a direct child of an `*ast.AssignStmt` that
`ast.Inspect` reaches and folds with the *same* `reassemble`), so folding skips
it. Consequences:

- **No global dedup.** The existing candidate multiset is preserved exactly, so
  every `sqlparse`/`sqltext` test that counts candidates is unaffected.
- Folding dedups **within its own output** by text (`full == base` when a
  variable has no optional appends → one candidate).
- A folded candidate carries the **seed `:=` line** as its anchor — where the
  query originates. On the deskallocation shape the finding lands on line 271
  (the `:=`), not 277 (the `+=`).

**One honest caveat this rule does not remove.** In the *all-paths-append* shape
(`if c { q += A } else { q += B }`, both locking-without-order), the bare seed
value is unreachable, yet the expression walk still emits it and — if it is
itself a locking `SELECT` lacking `ORDER BY` — fires on it. That is a *pre-existing*
conservative fire of the expression walk (it has emitted every `:=` literal since
before folding existed), neither introduced nor removed by per-path folding.
§4.3's "skip the seed" only avoids folding *duplicating* it.

**Double-count fix.** Because there is no cross-pass dedup and a folded value can
be a superset of an already-firing seed at the same line
(`sql := "… FOR UPDATE"` then `sql += ";"` → walk fires on the seed, folding
fires on `"… FOR UPDATE;"`, same line, same message), `sql/locking-select-order`
MUST dedup its own `Match`es by `(Line, Message)` in `CheckFile` before
returning. Identical findings at one line are indistinguishable to the reader
and inflate a parity-port tally; collapsing them is correct and reinforces
deterministic output.

### 4.4 Placeholder honesty

An appended non-literal (`q += orderCol`) reassembles to `fw_expr`. The spec must
**not** claim this is always "unverifiable rather than assumed-safe": glued into
a position nothing validates, the reassembly routinely comes out *unparseable*
(`… WHERE id = ANY($1)fw_expr FOR UPDATE` — a real column name glued there fails
identically; position decides this, not the placeholder), and an unparseable
candidate is dropped silently by `lockingStatements` — i.e. *effectively
assumed-safe*. Where the same gluing lands in a position that still parses (e.g.
a dynamic `ORDER BY fw_expr` column), the placeholder *is* verified
conservatively (never matches a configured `unique_key_columns` name → fires
under the strict default). Both behaviours are inherited from the existing
placeholder mechanism, not new; the correction is only to stop overstating the
guarantee. The genuinely-dropped case is a disclosed miss, now measured for
three shapes (§9).

## 5. Doc-comment corrections (same commit)

Three comments would newly mislead and are corrected — each *scoped honestly* to
what C actually delivers (straight-line and `if`/`else` flow), not "handled":

1. `FromGoReassembled` doc ("literals split across statements … skipped
   entirely") → straight-line and `if`/`else` assignment flow is now folded
   (bounded, all-or-nothing); `strings.Builder`, loop/`switch`/closure flow, and
   mixed-independent-flag paths remain skipped.
2. `sqlextract` package `HEURISTIC LIMITS` → assignment-spread literals are
   folded by `FromGoReassembled` but still dropped by `FromGo` (byte-identical
   for `sqltext`); the residual gaps above remain.
3. `locking.go` `COVERAGE LIMIT` → the straight-line/`if`-branch blind spot is
   closed; the remaining holes (§9) are named so a clean run on a composed `.go`
   query is still not over-trusted.

## 6. Bounding

There is no silent cap (§4.2), no set-valued growth, and **no exponent that
depends on the source**. Emission is bounded by construction at **2 + 4×N** texts
per variable, where N is `maxEnumeratedPairs` (3): `full`, `base`, and the two
branch worlds of each enumerated complementary pair, each rendered minimally and
maximally — **fourteen** at the ceiling. Duplicate texts are deduped before
emission, so the realistic count is two or three. (This paragraph read "six …
at most one complementary pair" until 2026-08-02; the widened pair model landed
in `a5d4cc6` and revised §3, §9 and `locking.go` without revising §6, which is
the section a reader consults for the parse-cost ceiling.)

That is a hard ceiling rather than a tuned bound, and it is deliberately not a
knob. An earlier model enumerated `2^n` truth assignments over `n` complementary
names with a linear cover past a threshold, and the threshold became a
false-negative dial: lowering it from three to two silenced a hazard the pre-#42
model reported, and the cover itself could drop every world in which an
independent flag took one polarity. A bound that can delete a reachable world is
not a bound on cost, it is a hole in the analysis.

Nothing in the model can delete one now. Because emission only ever grows (§3),
the two failure directions are no longer in tension: the ceiling is set by parse
cost alone, and correctness does not depend on where it sits.

**Cost.** Each emitted candidate is one `pg_query` (WASM) parse in
`sqlparse.lockingStatements`, which has no volume guard of its own, so this
model's ceiling is the caller's parse budget — and those parses are what the
rule's pre-parse gate exists to avoid, on exactly the query-builder files this
model is aimed at. Fourteen per variable at the ceiling against the pre-#42 two
is the whole of the trade, and it does not grow with the source.

The block-level work is the pre-#42 untracking walk, unchanged. There is still no
second pass: the guard bookkeeping that three earlier revisions added — value
epochs, alias facts, closure positions, a whole-block purity scan — is gone along
with the suppression it existed to justify, and the #72 fix deliberately did not
reintroduce it. (The rejected refusal design would have: it needed a whole-block
pre-scan for closure writes before folding, precisely the shape this paragraph
says was removed. Folding an IIFE inline needs no pre-pass — it is one more arm
in the statement walk that is already running.)

## 7. Testing (RED-first, two commits per project TDD rule)

- `internal/sqlextract/sqlextract_test.go` (RED first):
  - unconditional `x := <string expr>` / `x += <string expr>` folds to **one**
    candidate (the joined value);
  - the `x := …` / `if c { x += … }` shape produces the base-plus-appended
    joined candidate. **Deliberate deviation from the issue's literal "folds to
    one candidate":** under bounded per-path semantics the in-`if` case yields
    the `full` joined value *in addition to* the base the expression walk already
    emits, so the assertion is on **presence of the joined (`full`) candidate**,
    not an exact count of one. Approved in brainstorming.
  - the decorrelated-guard shape (`if c { += order }` / `if c { += lock }`) does
    **not** emit a lock-without-order value — the regression test for C over B.
- `internal/rules/sqlparse/locking_test.go`: the deskallocation shape **fires**
  (exactly one finding, on line 271 — the only firing candidate is the folded
  `full` value) with its `ORDER BY` removed, and **passes** (zero findings) with
  it present. A same-condition-guard fixture asserts **no** finding on safe code
  (the B-FP regression guard).
- `FromGo`'s behaviour and every `internal/rules/sqltext` test are unchanged
  (verified: `FromGoReassembled` has a single production consumer,
  `lockingStatements` at `source.go:141`, so added candidates reach only the
  locking rule).
- `make verify` green.

## 8. Boundaries preserved

`sqlextract` still knows nothing about rules. `FromGo` is untouched. The new
pass is confined to `FromGoReassembled` and reuses the existing `reassemble` /
`reassembleOperand` folding primitives, so there is one reassembly semantics,
not two. The `Match` dedup (§4.3) is local to `sql/locking-select-order`.

## 9. Coverage limits (disclosed, not silent)

C makes the straight-line and `if`/`else` slice visible. These remain
under-reported, and a clean run over a `.go` file that composes SQL is proven
only for the shapes above:

**"Disclosed" means COUNTED, and #311 built the counting.** It used to mean
prose in a doc comment plus a table a test reads, and the operator-facing channel
built to carry it — `formwork lint`'s #75 escape-hatch census — asked
`sqlextract.FromGo` for the answer. Neither locking rule sources through FromGo.
Both go through `FromGoReassembled`, which resolves the only two shapes FromGo
declines (a `fmt.Sprint{,f,ln}` with a literal first argument, and a one-sided
`+` chain) into `fw_expr` text and analyses them. So the channel was wrong in
both directions at once: every line it printed denied analysis of a composition
the rule reads — `formwork check` failing on `db/q.go:6` while `formwork lint`
called that same line "not analysed by this rule" — and none of the limits below
produced a line at all. Four files each hiding an unordered locking SELECT
behind a `strings.Builder`, a loop, a called closure and `lockIt(&q)` gave exit 0
and an empty census.

`FromGoReassembled` now returns a `Site` for each composition it declines,
`sqlparse.UnreadableSites` filters them to the SQL-shaped ones, and
`sqlparse.CensusSites` owns the rule-type → extractor mapping the census read
off a type name.

**`formwork lint` asks that seam as of #311.** `enumerateEscapeHatches` calls
`sqlparse.CensusSites` rather than keying on a `sql/` type-name prefix, so the
census sources through the same rule-type mapping the rules themselves do.
`census_wiring_test.go` holds this paragraph and the call graph to each other in
both directions: it fails if the wiring is removed while this text claims it is
present, and it failed while the wiring was absent and this text disclosed the
gap. Neither the prose nor the call can move alone.

Three properties make the sites usable rather than merely present:

- **Anchored at the write, never at the seed.** The expression walk emits every
  seed literal whatever the fold does, so a seed that is itself an unordered
  locking SELECT fires at its own line; a site there would say "not analysed"
  about a line the same run just failed on.
- **Carrying the text**, so a consumer can filter to SQL rather than listing every
  untracked path and format string in the repo.
- **Three verdicts, not two.** A composition can be read whole (no line), read
  not at all (the SILENT shapes), or read IN PART — a disqualified IIFE and a
  literal invoked in an `if` condition both drop their own appends while the
  variable outside stays tracked, so a world IS emitted from part of the query.
  `UntrackReason.Partial` carries that distinction and
  `TestPartialFlagMatchesTheDisclosedVerdict` fails if it ever disagrees with the
  verdict `locking.go` discloses.

1. **Mixed-optional-append hazard** — an optional lock and an optional order on
   *genuinely independent* conditions; the hazard needs one on and the other off,
   which neither `full` (both on) nor `base` (both off) shows. Two guards split
   across a **single flag's opposite polarities** (`if !x {+= ORDER BY}` / `if x
   {+= FOR UPDATE}`) were previously part of this miss and are now **covered**
   (§3): the pair is enumerated one branch at a time, and the x=true world is a
   lock with no order. That coverage extends to **a pair one nesting level down**
   — two appends on `b` and `!b` inside a shared `if useTx { … }` share a guard
   prefix and are paired — and to **every pair on the variable**, up to three,
   each enumerated separately. Two sub-cases remain:
   - **A pair whose appends sit under DIFFERENT guard prefixes** — `if a { if b
     { … } }` against a bare `if !b`. Fixing `b` forces neither, since `a = false`
     skips both, so this is not a pair and is not enumerated.
   - **Complementary pairs past the third** — truncated for parse cost (§3), so a
     hazard needing one branch of a fourth pair is under-reported. The variable
     still gets `full`, `base` and the first three pairs' worlds.
2. **Loop/`switch`/`select`-built** queries **and `if`/`else` where both branches
   append** — the variable is untracked (§4.1). Closures are no longer wholly on
   this list: an immediately-invoked literal is folded inline **when its body is
   fully modellable (§4.2)** — every statement is an admitted shape, AND no name
   it binds is also one it appends to. A body failing either — a plain call, a
   loop, a `return`, a `switch`, or one that binds and appends the same name —
   is left opaque and behaves exactly as pre-#72 (see the false-positive/
   silent-miss pair this reopens, below). Inlining is also **position**-scoped,
   not only shape-scoped: only a literal invoked as a STATEMENT is recognised.
   One invoked in an `if` **condition** — `if func() bool { q += … }() {` — is
   refused by `iifeBody` on its result before shape is even considered, and is
   reached through `IfStmt.Cond`, which no write-detection path inspects. It
   runs unconditionally, so it fabricates in both directions exactly like a
   disqualified body (second false positive, below). A named closure this pass can SEE
   being called is on this list too, and is a miss rather than a fabrication:
   the variable is untracked and nothing is emitted (#72, widened across every
   spelling this pass can see by #337, §4.2). Not across the one it cannot: a
   name handed to a call — `run(add)` — is not an invocation to a parse-only
   pass, so that spelling stays tracked and fabricates, the fourth false
   positive below. For every OTHER closure the
   enclosing walk keeps folding, which is correct whenever that closure is
   conditionally called, never called, or created after the value is used — the
   emitted value is then the not-called path, which the program really produces.
   What such a closure appends is genuinely unmodelled, a miss in the ordinary
   sense.
3. **A backward `goto`** — a `lbl:` / `goto lbl` pair is a loop whose appends run
   once per iteration, and this pass renders each append exactly once. The label
   is untracked, exactly as `for` is (§4.1), so this is a miss. The **forward**
   direction is no longer on this list: the jumped-over world is emitted (§4.1,
   #73), and it was the silent one — an unordered locking `SELECT` on the skipped
   path that no world modelled. A `goto` whose label is not defined in the
   statement list it sits in is modelled by the enclosing list that does define
   it, and nowhere else.
4. **A write through an address this block does not resolve** — a helper handed
   `&q` (`lockIt(&q)`), an address stored or returned, or a pointer mentioned
   anywhere but as `*p`. What the callee writes is not in this file and needs the
   escape analysis §2 declines, so the variable is **untracked** where the escape
   provably runs: a miss, and an honest one, where it used to be a wrong emit in
   both directions. A **write through a dereference** in the block untracks on
   the same terms and for the same reason (#74). Neither is RESOLVED: a
   `p := &q` whose every other mention is a dereference is exempt from the
   escape untrack — this block's text decides what `p` names — but `*p op= …` is
   not read back as `q op= …` and nothing is emitted for it. This item claimed
   otherwise until #312; §4.1 carries the correction and `locking.go`'s COVERAGE
   LIMIT always had it right. Two costs remain, stated rather than argued
   away: an unconditional `readOnly(&q)` — a callee that takes the address and
   does not write — loses a world that was real, which parse-only cannot tell
   from a writing callee; and an escape **under a branch** does not untrack, so
   an `if b { mutate(&q) }` still emits the not-called world (correctly) and
   models the called one not at all. Two residuals are open on this item and
   are not covered by the above: an escape in **both** arms of an `if`/`else`
   provably runs yet is not untracked, because telling it from a one-arm escape
   means proving the two arms escape the SAME name and the classifier is keyed
   by bare name with no scope stack; and an escape whose statement is not in the
   provably-evaluated set is left alone even where the callee does write.
5. **Mid-block use before a later append** — block-end emission analyzes only the
   final value of a variable that was passed to a query earlier in the block.
6. **Unvalidated concatenation seams** (§4.4) — the fold (and the placeholder
   mechanism it uses) concatenate text with nothing checking whether the
   result parses, so a composition is sometimes dropped as unparseable.
   Measured, not merely named: a differential across 2,035 generated files
   plus `examples/` and 8,662 files of the validating port found the fold base-equal
   everywhere except three append shapes, fifteen files total, all among the
   generated files — none in `examples/` or the validating port. The three share a
   *symptom*, not one cause: an `ORDER BY` built from two non-literal pieces
   joined by a bare space renders `ORDER BY fw_expr fw_expr`, which fails
   identically with two *distinct real* column names in the placeholders'
   place — `ORDER BY` needs a comma there, and a space was never going to
   supply one, so no placeholder is involved; a `fmt.Sprintf`'d `ORDER BY`
   glued straight onto a preceding digit renders `1ORDER`, a
   digit-then-keyword lexer collision that fails the same way with a real
   column name, well before the placeholder that follows it; and a literal
   `"%%"` append glued onto quoted text renders a bare `%`, with no
   placeholder anywhere in the rendered text. All three are pre-existing —
   nothing #72 introduced — so this is the miss's first measured bound, not a
   new one.
7. **`strings.Builder`** — the rule's verdict is unchanged and its visibility is
   not. A builder has no tracked name to untrack: `var sb strings.Builder` binds
   no string, `sb.WriteString(…)` is a call rather than an assignment, and
   `sb.String()` returns a value the fold has never modelled, so the composition
   is invisible to the whole mechanism rather than dropped by it, and the rule
   emits nothing — `locking.go`'s `SHAPE strings-builder SILENT #311`. It is
   **counted** all the same, and that is what #311 changed: `builder.go` walks
   the write calls on any name a declaration or a signature says is a
   `strings.Builder` or a `bytes.Buffer`, `FromGoReassembled` returns one `Site`
   per builder anchored at its FIRST write, and `sqlparse.UnreadableSites`
   filters those to the SQL-shaped ones, so `formwork lint` reports a line per
   builder that accumulates a query. The text it judges SQL-ness by is every
   literal written in, plus the literal text of any name written in that the
   enclosing body binds, plus the file's package-level `const`/`var`
   declarations under those — a query is usually a package constant, and reading
   only the body left this shape as silent as it was before #311 for the
   commonest spelling of it. **Two residues, and they are silences of the census
   rather than claims about what was read.** A seed declared in a SIBLING FILE
   of the same package is not resolved, this pass reading one file at a time
   (§2); and a builder reached some other way — a struct field, a value a helper
   returns — is not recognised as one, deliberately, since over-recognising puts
   a census line against every `.WriteString` in a repo, which is the flood that
   makes the channel unreadable.

**A clean run is not silence about the whole query.** Whatever the fold does or
does not emit for a variable, the expression walk still emits the `:=` seed
literal and the rule analyzes it on its own (§4.3). So a query whose composition
this list cannot model is analyzed *as if the seed were the whole query* — which
can both miss a hazard a later append introduces and fire on one a later append
cures. "Not analyzed" describes the composed worlds, never the seed.

**Item 2's disqualified-IIFE case and item 2's condition-position literal go
both ways; the rest are misses. There are four false positives.**
The list above mostly under-reports:
the fold models fewer worlds than execution reaches, and a clean run on those
shapes means "not analyzed", not "safe".

A **named closure called unconditionally** — `add := func(){ q += … }` then
`add()` — used to head this list, and the spellings this pass can SEE being
called have left it: they are untracked and emit nothing (item 2, #72/#337).
One spelling has not left it, and it is the **fourth** entry below — the
closure's name handed to a call, `run(add)`, where whether it runs is decided in
another function. It is recorded here because the reasoning that kept the whole
class on this list for months is still right about the shape it was reasoning
about: a
statement-position immediately-invoked literal is folded inline because its
execution *is* provable (§4.2), and provable execution is necessary but not
sufficient, as the second false positive below shows.

The first is **an immediately-invoked literal whose body is disqualified**
(item 2, §4.2) — a single plain function call in the body is enough. The
literal is left opaque, not untracked, so the variable stays TRACKED and any
append surrounding the closure still applies while every append INSIDE it
silently vanishes. Unlike the misses above, this one goes BOTH ways, measured at the
gate (`unique_key_columns: [id]`):
`func(){ side(); q += " ORDER BY id" }()` then `q += " FOR UPDATE"` outside
fabricates an unordered value where the real one is ordered on every path —
**1 finding**, a false positive; `func(){ side(); q += " FOR UPDATE" }()` alone
drops the only candidate that would have fired on a genuinely unordered locking
SELECT — **0 findings**, silence on the #72 hazard itself, reopened by any
disqualifying statement in the body. Widening `iifeShapesAdmissible` to admit
more per-statement shapes is what six review rounds on that predicate already
measured the cost of; the all-or-nothing refusal is what makes a disqualified
body provably base-equal at all.

The second is **a function literal invoked in an `if` condition** —
`if func() bool { q += " ORDER BY id"; return true }() {`. It is not a
disqualified body: `iifeBody` refuses it on its *result* before shape is
considered, and it is reached through `IfStmt.Cond`, which no write-detection
path inspects — `foldStmts` reads `Cond` for guards only, and `untrackAssigned`
stops at `*ast.FuncLit`. The condition of an `if` is evaluated unconditionally,
so this fabricates both ways, measured on the shipped extractor: with the
`ORDER BY` inside and a `FOR UPDATE` outside it emits
`SELECT id FROM t WHERE s='x' FOR UPDATE` — unordered, where every real path is
ordered; with the lock inside and nothing outside it emits **no folded candidate
at all**, so a genuinely unordered locking SELECT is never analyzed. This shape
was named in #72's first comment as one the narrow fix would miss; that comment
was later retracted **wholesale** to kill a rejected design (§10), and the shape
inventory went with it — which is why it survived the whole of #72 undisclosed.
Closing it needs `Cond` to become a site the write detection visits, which is
not a widening of `iifeShapesAdmissible` and is tracked on #72 rather than here.

The third is `base`. Under a complementary pair (`if a` / `if
!a`) the "no optional append taken" world is unreachable, and the fold emits it
anyway — **#42, unfixed and disclosed**. Suppressing it needs proof that the two
`if`s read one value; §3 records why a parse-only pass cannot carry that proof,
and why even a correct proof would not cover a query observed between the two
branches. Four review rounds of suppression each deleted a reachable world
instead and silenced a real deadlock hazard.

The fourth is **a closure whose NAME is handed to a call** (item 2, §4.2) —
`add := func(){ q += " ORDER BY id" }` then `run(add)`, against
`func run(f func()) { f() }`. It fires on a query every real path orders. It is
the one call spelling `invoked.go` deliberately does not read, because the same
text is `register(add)` against `func register(f func()) {}`, where nothing
invokes the closure, the unordered locking SELECT is the real value and that
finding is the hazard. **#337 — DECIDED:** the false positive is kept, and the
measurement is the reason — `run(add)` and `register(add)` both measure
**1 finding** at `unique_key_columns: [id]`, so untracking on the escape deletes
the second to delete the first. Telling the two apart is the cross-function flow
§2 declines outright, and a same-file-only version of it
decides the verdict by where the helper happens to be declared: folding for a
helper in a sibling file of the same package, untracking for the same helper
moved into this one. Unlike the first and the second this one does **not** go
both ways — with the lock INSIDE the closure the gate is silent for every call
spelling, bare `add()` included, which is item 2's own miss and not this
shape's. So it over-fires and does nothing else, and the marker below is a real
answer for it. `locking.go` discloses it as `SHAPE closure-name-escape FIRES`
and as item 5 of its own list (#337).

So the trade for the **third** is taken on purpose and in one
direction only: a world that may be unreachable is a **visible** finding, and a
`formwork:allow` marker on the line suppresses it while `formwork lint`'s
exemption enumeration keeps the suppression in view. A world that is reachable
and unemitted is silence on a deadlock hazard, and nothing surfaces it at all.
If a finding here has no reachable path, the answer is the marker — not a query
rewritten to please the analyzer. It settles the **fourth** on the same terms,
which over-fires and nothing else. **The first's and second's silent directions
have no such answer**: there is no finding to suppress, only one that never
fires. Those two are the ones a reader auditing this rule should hold onto —
they are the shapes where a clean run proves nothing at all.

## 10. Revision note

§§1, 3, 4, 5, 6, 7, 9 were revised after a four-lens adversarial review
(claims / logic / outcomes / edge-cases). The material change: emission moved
from full per-path enumeration to **bounded all-or-nothing (C)** to remove a
false positive on provably-safe code (decorrelated same-condition guards) and a
cap-driven silent give-up. Confirmed unchanged by review: the factual claims of
§1/§4 (single consumer, the walk already emits the seed, the file-level lock
gate, the deskallocation trace and its `= ANY` non-exemption), and that both
acceptance outcomes hold. Also folded in: the §4.4 placeholder-honesty
correction, the §4.3 double-count dedup, the §4.2 closure/emit-timing/determinism
fixes.

**Plan-time refinement (2026-07-29).** While drafting the implementation the
mandatory `if`/`else` case (both branches append) was changed from *forked* to
*untracked* (§4.2, §9). Forking a rare shape would make the accumulators
set-valued and reintroduce `2^m` bookkeeping; untracking keeps them single
strings (≤ 2 candidates, no cap) at the cost of one more disclosed miss, in the
sound (under-report) direction. This narrows what the AskUserQuestion option C
called "mandatory if/else choices still enumerated" to "not enumerated —
untracked"; the change only *removes* coverage of a rare shape, never adds a
false positive.

**Closure correction (2026-08-02, #72).** §4.2 claimed a closure that mutates an
outer variable was "simply unmodeled — a disclosed miss". False: the appends were
dropped while the variable stayed **tracked**, so an immediately-invoked literal
made the fold emit a value no path produces — a spurious finding when the closure
held the `ORDER BY`, silence on an unordered locking `SELECT` when it held the
`FOR UPDATE`. Reproduced on `main` at the extractor and the gate.

**A first fix was designed, planned, adversarially reviewed, and rejected**, and
it is recorded here because the reasoning is the useful part. It refused any
variable a nested closure writes (a per-block poison set). Four review lenses plus
direct measurement found it deleted correct findings: for a closure that is
conditionally called, never called, or created after the value is used, the value
assembled from the appends outside it **is** a real path, so the pre-fix finding
was a true positive. Across fifteen shapes refusal removed ten findings, eight of
them true positives, against the one false positive it targeted — and being keyed
on the bare name across a whole function body, it manufactured a new silence when
a closure wrote `q` in one branch and an unrelated `q` lived in a sibling branch.
It could not fix the direction that matters either: the gate fires per emitted
candidate, so subtracting candidates only ever subtracts findings. It was the
move `foldworlds.go`'s "EMISSION ONLY EVER GROWS" exists to forbid, proposed by
someone who had just quoted that comment into this spec.

**What landed instead** is inline folding of the one closure shape whose
execution is provable — `func(){ … }()` — so the real value is reconstructed
rather than guessed at. Both directions of #72 are fixed, and unlike the refusal
the silent one now **fires**: the deadlock hazard the issue was filed about is
caught, not relabelled. Every other closure keeps folding, so no true positive is
lost. Also folded in: `ast.Unparen` on every LHS identifier, closing a
fabrication via `(q) += …` that needed no closure at all and reached plain block
level; §6's stale "six texts per variable" ceiling (fourteen since `a5d4cc6`);
§9's disclosure that the seed literal is analyzed regardless of what the fold
emits; and `goto` and taken-address writes added to §9 as pre-existing misses,
filed separately. §§4.1, 4.2, 6, 9 revised.

**Post-#42 correction (2026-07-30).** Code review of the #42 fix found its
base-suppression unsound in the false-negative direction, and §3 asserted a
soundness property (“base is never suppressed on a reachable path”) the
implementation did not have. Both are corrected above. The material change:
complementary guards no longer *delete* a world but *select* which worlds are
reachable — per-append guards, one branch enumerated per complementary name,
minimal and maximal worlds per assignment — and guard collection is narrowed to
stable, named, non-rebound, non-Init-bound conditions. Net effect: the #42 FP
stays fixed, five reproduced false negatives are closed, the infeasible `full`
under a complementary pair is also dropped, and §9 miss 1's opposite-polarity
sub-case becomes covered. §§3, 6, 9 revised; the remaining guard gap
(indirect mutation) is disclosed under §9 miss 1 rather than left implied.

**Second-pass correction (2026-07-30).** A workflow code review of that
correction reproduced seven defects in it, all in guard collection or the world
enumeration, and all fixed here with a test each:

- guard invalidation was **scope-wide and position-insensitive** — computed for
  the whole block and applied after the walk, so a write *after* the query was
  built reinstated the infeasible `base` (#42 again), while a write *inside* the
  first branch, above its own append, was not counted at all (a reachable
  both-branches world deleted). Replaced by per-read **epochs** (§3): a guard
  records the write count of the value it read, and complementarity needs the
  same value *and* epoch, which is positional by construction and needs no
  "already recorded" special case;
- writes through a **`for … = range` clause**, a **taken address**, and a **func
  literal** were not counted as writes at all — each one a deleted reachable
  world, the false-negative direction;
- **field guards** (`if o.Ordered` / `if !o.Ordered`) stopped proving
  complementarity when collection was narrowed to bare idents to exclude calls,
  reinstating #42 for the commonest guard shape in a query builder. Collection
  now takes a *stored-value path* (ident or field chain) and still excludes
  calls;
- a **nested** optional append kept no record of its enclosing branch, so the
  enumeration paired it with worlds that fix that branch false — a lock that
  needs `a` appearing in the `a = false` world, an invented FP. Guards are now a
  conjunction, recorded outermost-first, and only a sole-guard append proves
  complementarity;
- past `maxGuardPairs` the enumeration **fell back to plain `full`/`base`**,
  which is #42 exactly. Replaced by a linear cover of feasible worlds (§6).

**#42 revision (2026-07-31).** [#42](https://github.com/buildfoundry-nz/formwork/issues/42)
reported the `base` false positive this design predicted (§3): under a
complementary pair (`if a` / `if !a`) the "no optional append taken" world is
unreachable, and emitting it fires on a query ordered on every real path. Four
successive attempts to suppress it — plain complementarity, per-value epochs,
epochs plus correlation and closure positions, and a whole-block purity scan with
widening — were each reviewed adversarially and each found to **delete reachable
worlds**, silencing genuine unordered-locking `SELECT`s on ordinary Go (a helper
call handed an options struct, a method value, a pointer in a composite literal,
an embedded field, an alias made in a closure). 37 defects across four rounds.

The material change: **suppression is abandoned and the sound half kept.** The
fold emits `full` and `base` unconditionally and *adds* the two branch worlds of
a single complementary pair, so the emitted set is a strict superset of the
pre-#42 model and no shape it reported can go silent. That closes §9's
split-lock/order miss, which was the coverage this work actually found, and
leaves #42's false positive standing and disclosed (§3, §9, and `locking.go`'s
`COVERAGE LIMIT`) rather than trading it for silence.

Removed with the suppression: value epochs, truth-assignment masks and their
linear cover, `maxGuardPairs`, alias facts and spans, guard prefix signatures,
scope-ownership collection, and the per-statement and whole-block write scans.

**Pair-collection revision (2026-08-02).** A fail-open review of the widening
model above — the first review of the code that actually *ships*, since all four
earlier rounds reviewed suppression designs that were discarded — found two
silent misses, both in what counted as a *pair*. Neither was a suppression bug;
both were exclusions inherited from the abandoned attempts and never re-examined
once suppression was dropped.

- **Sole-guard collection missed a pair one nesting level down.** Two appends
  under a shared `if useTx { … }` carry guards `useTx && a` and `useTx && !a` —
  identical prefix, opposite last polarity — and were not paired, so `full` was
  the only emitted world and the `useTx && !a` hazard reached nothing. Pairing
  now keys on *(prefix, last guard)*, which admits this and keeps the
  differing-prefix exclusion the sole-guard rule was actually written for. No
  existing test changed, which is the evidence that nothing pinned the old rule.
- **A second pair removed analysis.** Branch worlds were emitted only when
  exactly one pair was named, so more complementary flags bought *less*
  coverage. Every pair is now enumerated separately (each world fixes one truth
  value, so no cross-product is formed), capped at three for parse cost, and the
  cap truncates rather than collapses.

The `sole-guard` phrasing in the 2026-07-30 entry above is a record of what that
revision did, not of current behaviour. Cost of the second change: one further
disclosed false positive on correlated flags (`b := a`), same class as #42 and
disclosed with it.

`internal/sqlextract/fold.go` is 304 lines of code against main's 133; the
abandoned suppression attempts peaked at 618.

**`goto` and taken-address writes modelled (2026-08-24, #73/#74).** §9 items 3
and 4 had recorded both as pre-existing wrong emits: a write the walk cannot see
while the variable stays tracked, which is the same root the closure correction
above was about. Both bug reports proposed **untracking** as the conservative
cure, and both times that is the wrong direction for the same reason the poison
set was — the gate fires per emitted candidate, so a refusal only ever subtracts
findings and can never reach the silent direction. What landed is modelling:

- A forward `goto` marks the statements it jumps over as optional, so the
  skipped world is **added** beside the fall-through one and the unordered
  locking `SELECT` on the skipped path now fires. `full` is untouched, so no
  world the list emitted before is removed. A forward-jump label is folded
  through rather than untracked. A backward jump is still a miss (§9 item 3).
- A block that writes through a dereference untracks every name whose address
  it takes. RETRACTED, and the retraction is the point of the entry two below:
  this bullet said `p := &q` "is resolved when every other mention of `p` in the
  block is a dereference, and `*p op= …` folds as `q op= …`", and no such
  resolution was ever built. `p := &q; *p += " FOR UPDATE"` emits nothing; the
  identical query without the pointer fires. The only thing the identifier-
  occurrence allowlist decides is the ESCAPE exemption (`escape.go`,
  §4.1) — whether `&q` counts as leaving the block — and even for a name it
  exempts, a dereference WRITE still untracks. The prose came from the
  attempt arc 2 reverted, and `locking.go`'s COVERAGE LIMIT — which says
  UNTRACKED, NOT RESOLVED — is the accurate text (#312).
  Every other `&q` is an escape and **untracks**, and
  refusal is correct there precisely because it is not a refusal of a real world:
  where the escape provably runs, the value assembled without the callee's writes
  is produced by no path. Under a branch it does not untrack, because there the
  not-taken path is real — and neither does it in a position inside a statement
  that does not provably run: a loop body, a `switch` case, a `select` clause, a
  `defer`/`go` call, a `return` expression. Reading the branch context alone
  deleted exactly the worlds those paths produce, the canonical builder
  `for _, f := range fs { addFilter(&q, f) }` most of all.

A dereference target disqualifies an IIFE body (§4.2's admitted set). It was
added against the enclosing-scope fabrication — the alias map is keyed by bare
name with no scope stack, so an inlined `*p += …` folded into an enclosing
variable that merely shared a name with the closure's own — and that hazard is
now closed at its source instead, by collecting bindings only from the top level
of the block being folded. The refusal is kept because admitting a dereference
target is a widening of the admitted set rather than a fix, and it is not free:
`p := &q` then `func(){ *p += " ORDER BY id" }()` drops an append that really
runs, the ordinary disqualified-IIFE cost (§9, false positive one) reached
through a pointer. Open on #74.

A measurement stood here — "a seven-shape corpus … 4 findings, **0 false
positives**, both previously silent hazards firing" — and is REMOVED. No corpus
of that name exists in the tree; the phrase occurred nowhere but in the sentence
itself, and occurs now only in this retraction and the entry that replaces it.
The escape half it measured was not built until #310 two days later,
and one of its two "previously silent hazards" is still silent by design: a
`lockIt(&q)` composition is untracked, which is an honest non-analysis and not a
firing. The measurement that replaces it is in the #312/#313 entry below, and it
is one this repo runs. §§4.1, 4.2, 9 revised.

**The escape half, actually built (2026-08-26, #310).** The entry above
describes §4.1's escape model in the past tense. Only its dereference-write
clause had shipped: `aliasUnsafe` returns nil unless the same block also
contains a `*x =`/`*x +=`, so `lockIt(&q)` — with no dereference anywhere in the
caller, the ordinary spelling and the one #74's own Scope names verbatim — left
`q` fully tracked and the fold emitted a world no path produces, in both
directions. `internal/sqlextract/escape.go` is the classifier that was missing:
every `&name` in a scope is either a resolvable local alias (`p := &q` or
`var p = &q` bound at the top level, every other mention of `p` a dereference,
left to #74's analysis) or an escape that untracks the name — at a provably-run
position only, which is an assignment's target or value, an expression
statement, a channel send, a `var` initialiser, an `if` Init or condition, and a
labelled statement. `for`/`range` bodies, `switch` and `select` clauses,
`defer`, `go`, `return` expressions, `if` arms, nested blocks and func-literal
bodies contribute nothing, each for the reason §4.1 gives.

§4.1's OTHER half — resolving `*p op= …` back to `q op= …` for a resolvable
alias — is deliberately not built. No filed issue asks for it; it opens a new
emission path where untracking only ever removes one; and `locking.go`'s shipped
COVERAGE LIMIT already tells its reader that a pointer-mutated query is
UNTRACKED rather than resolved. An address escaping in BOTH arms of an
`if`/`else` is likewise not untracked, which is what that same COVERAGE LIMIT
says: telling it apart from a one-arm escape means proving the two arms escape
the same name, and this classifier is keyed by bare name with no scope stack.

Twenty-six mutants over the new branches, each killed by a named test; the
positions that must KEEP folding are pinned one test per position, because
untracking any of them re-makes the first design this section records as
rejected. §4.1 named the escape model but stated its branch condition as "no
enclosing `if`-without-`else`", which is not what shipped — NEITHER arm of an
`if` is searched, with or without an `else`; that and the rest of §4.1's pointer
prose are corrected in the entry below.

**The disclosures match the code (2026-08-26, #312/#313).** Two documents
described this fold and neither described the shipped one. That is not a
documentation lapse on top of the work; it is the same defect the work is about,
one level up — a claim nothing executes, drifting in the direction that
misleads. `locking.go`'s COVERAGE LIMIT block declared #72 and #73 OPEN and
unmodelled seven and fourteen minutes after each landed, because each fix lived
in `internal/sqlextract` and the prose lived in `internal/rules/sqlparse`. This
spec carried the reverted attempt's pointer model in three places and a
measurement of a corpus that does not exist.

What was wrong, and is now what the code does:

- **`*p op= …` does not fold as `q op= …`.** §4.1, §9 item 4 and the 2026-08-24
  entry all said it did; nothing ever resolved a dereference. RETRACTED rather
  than implemented, for the reason the #310 entry gives: it opens an emission
  path where every other mechanism here only removes one, and `locking.go` — the
  document adopters are actually pointed at — had it right all along.
- **The escape narrowing is a list, not a principle.** §4.1 said "no enclosing
  `if`-without-`else`"; the shipped classifier searches an assignment's targets
  and values, an expression statement, a channel send's operands, a `var`
  initialiser, an `if`'s Init and condition, and the statement under a label,
  and nothing else — both arms of every `if` included.
- **A named closure this pass can SEE being called untracks.** §4.2 called it
  "one honest residue" that "does run, so the value emitted for it is
  fabricated", and §9 led its false-positive list with it. Closed by #72 and
  widened by #337 across every spelling the pass can see. **Not across the one
  it cannot** (#337): the closure's name handed to a call, `run(add)`, is not an
  invocation to a parse-only pass, so it stays tracked and fires on a query
  ordered on every real path. **#337 — DECIDED:** that is kept and disclosed
  rather than fixed — `register(add)` against a helper that never invokes f is
  the same text here and there the finding is real, and `run(add)` and
  `register(add)` both measure **1 finding** at `unique_key_columns: [id]` — so
  §9's list is four, with the escape entry last and the ordinals through
  §§4.2, 9 reading that way.
- **`ast.Unparen` on `foldAssign`'s LHS never covered `for (q) = range m`.** A
  `RangeStmt` reaches no LHS at all; `unmodelledWrites` is where that spelling is
  read (#314).

What stops the recurrence is the second half, and it is a structure rather than
a resolution. `locking.go`'s block now carries one machine-readable SHAPE line
per disclosed composition, and `internal/rules/sqlparse/locking_coverage_test.go`
runs every one of them through the real rule and the real fold. Three verdicts,
because two of them are zero findings and only one is an answer: FIRES, PASSES
(a world was folded and judged safe), SILENT (no world was folded — not
analyzed). The reasons the fold declines on are a table in
`internal/sqlextract/coverage.go` produced by the classifiers themselves, and
every one of them must appear in that block as a SILENT shape citing the same
issue. Prose, test and the operator-facing channel read one list.

**The measurement, which replaces the seven-shape one.** Nineteen compositions at
the gate (`sql/locking-select-order`, `unique_key_columns: [id]`), one per
disclosed shape, run by `TestCoverageLimitDisclosesWhatTheRuleDoes`:
**10 FIRES, 1 PASSES, 8 SILENT**.
Each verdict is read out of the block rather than restated here, and the tally
on the line above is read out of this file by
`TestFoldSpecMeasurementMatchesTheGate` and compared against the same run — so
it is reproducible by name AND wrong here rather than wrong in a quotation of
it, which is the property the sentence it replaces did not have. Two mutations show it measures behaviour and not text: reverting
#73 in `internal/sqlextract` (`skipped := jumpedOver(stmts)` back to an empty
map) turns the `forward-goto` line from FIRES to PASSES and reddens it, and
disclosing `deref-write` as PASSES reddens both the verdict assertion and the
untrack-reason one. §§4.1, 4.2, 9 revised; the 2026-08-24 entry above is
corrected in place.
