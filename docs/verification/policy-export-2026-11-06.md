# Policy exports: Cerbos and the Cedar policy language, on admin

Question: on real code, how much of a service's access guards do the
exports translate, and do the exported policies decide every realizable
case as the rules' own conditions do (`policy_inventory_plan.md` Part 3,
R7)? (The Cedar policy language, not CEDAR, Khan's analytics pipeline.)

## Revisions and commands

- codetospec working tree on `ccc0566` with `policy_inventory_plan.md`
  Parts 1-3 (FIX.md R1-R3 included); webapp `c55b04b0090`; cedar-go
  v1.9.0; Cerbos built from github.com/cerbos/cerbos `6340b18`
  (`go build ./cmd/cerbos`).
- A scratch copy of `~/spec-graphs/admin`, rerun with the remap's flags
  (`scanwebapp.sh admin --quality --crosscheck --acceptance
  --write-domain-model`).
- `codetospec export --out <copy> --to <dir> --format cerbos` and
  `--format cedar-policy`; `cerbos compile <dir>`; a temporary test
  running `cedar.Authorize` on every case `Operation.Cases()` gives,
  removed after the run.

## Results

- Exported: 18 operations, 11 deny rules; 21 guards not translated:
  20 call a check outside the pinned vocabulary (admin's own wrappers,
  `actorHasGlobalPermission` and its siblings, whose scope lives in their
  body), 1 is shared middleware. Their operations are left out whole.
- Cerbos: `cerbos compile` accepts the policy (CEL included) and runs the
  generated suite: 22 tests, 22 OK.
- The Cedar policy language: `Export` parsed the policies and the schema
  with cedar-go and validated each policy against the schema (strict).
  `cedar.Authorize` on the 22 realizable cases: 0 disagree with the
  rules' conditions.
- Both suites are decided by the oracle (`condition.Evaluate` on the
  rule's condition), not by either rendering.

### Cost of the rerun

admin's warm rerun spent 80,650 crosscheck and 116,957 acceptance tokens:
both keys fold in a rule's nearest tests, which FIX.md R3 and the
calls-through-variables change gave admin's role and capability rules.
One-time.

### districts, joined with admin's capabilities (R8)

`export --out <districts copy> --format cerbos [--capabilities-from <admin
copy>]` (the districts copy rerun with the policy inventory's facts):

| | alone | joined |
|---|---:|---:|
| operations exported | 166 | 224 |
| deny rules | 37 | 95 |
| guards not translated | 205 | 147 |

`cerbos compile` on the joined export: 296 tests, 296 OK; the Cedar
policy language export of the same model passed cedar-go's parse and
strict validation.

## Limits

- Two services. A service's capability guards translate only joined with
  admin's capability facts (`--capabilities-from`).
- admin's own wrappers are the largest gap: pinning their meaning as data
  (Part 3, R4) would translate most of the 20.
- Equivalence covers the translation, not the code: whether each guard
  is the one the code applies is the extractor's job.
