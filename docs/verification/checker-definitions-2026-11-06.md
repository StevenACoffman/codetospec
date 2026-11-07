# Checker definitions: verified against the code, readable by people

Question: once a checkers file pins what a checker means, can codetospec
check that pin against the checker's code, word it for people who don't
read code, compare two readers' pins, and export more of what the code
decides without over-permitting? (Cerbos and the Cedar policy language,
not CEDAR, Khan's analytics pipeline.)

## Revisions and commands

- codetospec branch `checker-catalog` (working tree on `913c2ce`); webapp
  `c55b04b0090`, the revision of the previous measurement
  (`checker-recognition-2026-11-06.md`).
- Five scratch copies of spec graphs (users, coaches, assignments,
  ai-guide, districts) were re-extracted with this branch's webapp and Go
  extractors, then re-run with `LLM_BASE_URL` on a closed local port:
  - users, coaches, assignments and ai-guide were served from cache
    (0 tokens; ai-guide's one failed chunk was already failed);
  - districts' copy predated the extractor changes already on `913c2ce`,
    so 19 chunks and 4 domains missed the cache and failed. That costs
    LLM rules only: the exports read extractor rules and facts.
  - admin was not re-run: its role sentences changed (FIX.md R4), so its
    map prompts miss the cache, and a re-run needs a model.
- A/B: the committed binary (`913c2ce`) and this branch, both on the
  re-run copies, with the review's checkers files (kept outside this
  repository) and `--capabilities-from` admin's graph. `cerbos compile`
  ran on every Cerbos export, with a locally built `cerbos`.
- The review's 20 checkers files were converted to JSON. Each passes the
  new schema, and each graph's inventory is byte-identical from the YAML
  and the JSON.

## Exports

| graph | operations, `913c2ce` → branch | deny rules | permits restricted by principal | `cerbos compile` |
|---|---|---|---|---|
| users | 112 → 112 | 87 → 87 | 72 of 112 | 446/446 OK |
| coaches | 157 → 158 | 132 → 133 | 106 of 158 | 522/522 OK |
| assignments | 114 → 114 | 58 → 58 | 37 of 114 | 284/284 OK |
| ai-guide | 159 → 159 | 119 → 119 | 102 of 160 | 560/560 OK |
| districts | 297 → 297 | 149 → 149 | 106 of 302 | 1008/1008 OK |

- **The coaches operation** returns a membership check's boolean as its
  value: the check is the answer the field serves, so it no longer hides
  the operation as a check below its guards.
- **Caught during the measurement, fixed before these numbers.** The
  first `answers` rule also counted a check returned whole (`return
  check(…)` from a resolver returning a value and an error) and one bound
  with its error (`v, err := check(…); if err != nil { return … }`). In
  both, the check's error reaches the caller, so it gates. The first
  measurement exported three login operations that way, plus one
  ai-guide operation from a copy extracted before the fix. Now a check is
  an answer only when its error can't reach the caller: a single value,
  or an error assigned to `_`.
- **Principals.** Push handlers allow push alone, and login or
  service-admin guards allow users or services.

## Definitions (`policies --checkers`)

| graph | checks | with paths | not analyzable | pinned | coverage computed | covered | leaves grounded / ungrounded / unknown |
|---|---|---|---|---|---|---|---|
| users | 42 | 16 | 8 | 9 | 7 | 5 | 11 / 0 / 10 |
| coaches | 27 | 13 | 0 | 8 | 6 | 4 | 1 / 0 / 7 |
| assignments | 26 | 9 | 6 | 8 | 3 | 3 | 5 / 0 / 13 |
| ai-guide | 39 | 22 | 7 | 18 | 15 | 14 | 18 / 0 / 9 |
| districts | 46 | 17 | 19 | 26 | 14 | 6 | 30 / 2 / 46 |

The first coverage reading matched 1 of the users pins, against 5 now.
Reviewing each mismatch found three limits of the reading, all fixed:
- a scope matcher listed among a path's calls is the capability call's
  operand, not a term;
- a helper nothing pins is a condition on data, so it matches a relation
  part;
- a two-value checker's failure variable is its error (a system failure)
  when the same check has a decision variable on some path.

Of the 5 mismatches left on users, coaches and ai-guide:
- **2 are conservative by design:** a stated check under an `any` or a
  `not` leaves its path unmatched, never falsely matched.
- **3 are for review:**
  - a checker whose boolean is a stored setting, not a decision;
  - a decision made through a helper that isn't pinned while the pin
    names a leaf;
  - a pin alternative that no allowing path shows, which may be looser
    than the code.

Districts' 8 mismatches and 2 ungrounded leaves are left for the review
of its file.

## Two blind readers (C6, P3)

Five districts pins were held at their strictest reading because a
literal argument changes the decision. The current extractor records
the arguments, and three of the five checkers take the deciding value as
a literal at their call sites. Two readers re-pinned those three from the
code alone, blind to the existing pins and to each other, with `cases`.
- `policies --compare`: one checker is equivalent. For the other two,
  the readers differ only in a `login` conjunct, with a counterexample
  (logged out, holding the admin relations) that no request can reach.
  The stricter form was adopted, not as `approx`.
- Exported, the re-pinned clear operations decide by their call's
  literal (at least one administered school for a partial clear) instead
  of conjoining both readings.
- The visibility filter's re-pin uses `self`/`coach`, as an item filter
  means, and three operations whose guards use that filter's failure as
  a call-level check are now left out (297 → 294). They translated before
  only through relations invented for the item. 1024/1024 OK.

## Question noise (Q1, Q2; `make corpus`)

On the eight corpus outputs, `nfr_target` questions went from 3,732 to
20, and `condition_mismatch` questions from 2,525 to 802
(`internal/corpus/testdata/corpus.json`). Q2 also found that re-annotation
never cleared a stale disagreement.

## Limits

- The `--quality` evaluation on the labeled PROMISE set
  (`internal/quality/eval_test.go`) is built and its scoring pinned. It
  was not run: no model was configured here.
- Admin was not re-measured (see above).
- Coverage compares the checks each path requires: negations and scopes
  are not compared, and a stated check under an `any` leaves its path
  unmatched.
- The two-subject checkers of the review check a relation held by a
  named user who is not the caller. Two bound subjects (P7) serve self,
  coach and subject relations about the caller, so those checkers stay
  unpinnable.
- The cross-service join (P6) pins a check only when the target's
  answered check is about no user. The call's arguments don't say which
  user it would be about.
