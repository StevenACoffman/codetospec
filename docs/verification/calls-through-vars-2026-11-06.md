# Call facts through package-level variables holding a function

Question: once a package-level variable holding a function (webapp's
`var X = cache.Cache(_uncachedX, …)`, dataloaders, function literals) is
a caller and a callee in the Go extractor's call facts
(`policy_inventory_plan.md` Part 2, design 8), what moves: call facts,
domain placement (`internal/callers`), drift's callees, test reach,
journeys, and the cost of a warm rerun?

## Revisions and commands

- codetospec working tree on `ccc0566` with everything before design 8
  (FIX.md R1-R3, Part 1, Part 2 steps 1-2, the grounding change) as the
  baseline, then `extractors/go/vars.go` `callableVars` and `CallFacts`
  walking a callable variable's initializer; webapp `c55b04b0090`.
- Baseline: scratch copies of `~/spec-graphs/{districts,users}` rerun
  with everything but design 8. Then copies of those, rerun with it:
  `OUT_DIR=<copy> PKG_GRAPH=~/spec-graphs/pkg ./scanwebapp.sh <svc>
  --quality --crosscheck --write-domain-model`.

## A first version, rejected by this measurement

The first version made every variable initialized by a call a callee.
`var ErrNotFound = errors.New(…)` is one, so every function returning a
sentinel error "called" it: +701 call facts on districts, +728 on users,
and about 645 of users' 1,842 stamped rules changed their callees. Only a
variable whose type is a function (`types.Signature`) is callable now,
pinned by a test case.

## Results (the adopted version)

| | districts | users |
|---|---:|---:|
| call facts | 16,689 → 16,800 (+111) | 10,141 → 10,257 (+116) |
| rules | 3,909 → 3,909 | 3,071 → 3,071 |
| rules changing domain | 0 | 3 |
| rules whose callees change (`callees_digest`) | 127 | 175 |
| rules whose `tests` change | 20 (7 from 0) | 38 (22 from 0) |
| LLM tokens of the rerun | crosscheck 26,261 | reduce 9,092, crosscheck 62,305 |

- users' three moves are all rules citing `resolvers/acls.go`, from
  `password` to `resolvers`. That file holds users' access helpers behind
  `cache.Cache` variables. Before, only the password resolvers reached it
  directly, so `callers` placed it in `password`; now the cached helpers'
  callers (bio fields, settings, user deletion, homepage data, misc data,
  signup and login) are visible, they disagree, and the file keeps its
  directory domain: shared ACL helpers placed as shared.
- The tokens are one-time: the crosscheck key folds in a rule's two
  nearest tests, which change for the rules gaining test reach, and the
  moved rules' domains reduce again.

## Limits

- Two services. A variable holding a function from a factory that
  returns an interface is not a function type and stays out.
- Drift: a stored spec's `callees` were stamped without these edges, so
  the next `drift` after regenerating recomputes them; no `affected`
  report is measured here.
