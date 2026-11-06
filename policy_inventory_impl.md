# Implementation plan: policy_inventory_plan.md

Temporary (2026-11-06). How `policy_inventory_plan.md` gets built, step by
step; delete with that plan. Baseline: codetospec `ccc0566`, `make test`
green, `golangci-lint run ./...` 0 issues in every module.

## Working rules

- Every step: write the doc comment (Requires/Ensures) before the body,
  then the test, then the code; `go test` and `golangci-lint run --fix ./...`
  in each module touched (never relaxing `.golangci.yml`), then a review
  pause against `~/Documents/agent-orange/go-advice/summary_rules.md`, then
  the next step. A finding of the review is fixed before moving on.
- Real-output measurements run on copies under the session scratchpad,
  never on `~/spec-graphs` itself.
- No commit unless asked. Untracked plan files stay untracked.
- AGENTS.md wins where it is more specific than the go-advice (one root
  `Error` type is not this codebase's convention; `Requires:`/`Ensures:`
  contracts, explicit loggers and pure exporters are).

## Corrections to policy_inventory_plan.md found while planning

1. **`check_args` carries a capability's Go name, not its value.** The
   webapp extractor parses without type checking and skips `generated/`
   (`extractors/webapp/analyze.go:428`), where each service's capability
   variables live. The value is joined in the core from the Go
   extractor's `uses_value` facts (type-checked, generated files
   included) or from admin's capability facts.
2. **A marked wrapper's scope can't be derived from its body.**
   `actorHasGlobalPermission` loops over `permissions.UserScopesForCapability`
   and tests `scope.IsGlobal()` (`services/admin/resolvers/acl.go:112-146`);
   it calls no `acl` matcher. So `check_args` records the wrapper as the
   check, with its capability argument, and leaves `matcher` empty. A
   wrapper's meaning is pinned as data in the export vocabulary, like a
   service's own matcher (Part 3 R4), or the guard isn't exported.

Both go into the policy plan when the step that meets them is done.

## Steps

### S0. Prerequisites from FIX.md

- **S0.1 FIX R1, extended.** `internal/source.Excluded(rel string, exclude
  []string) bool`, deciding as `Walk` does (hidden or excluded element).
  `pipeline.extract` drops, before `graph.ValidateRuleFacts`, facts whose
  `Source.Root == ""` and whose path is excluded, for the kinds in one
  list in `internal/extract`: `rule`, `domain`, and this plan's new kinds
  (added to the list by the step that adds each kind). Count in
  `RuleFactsExcluded`; one warn line each. Tests: walker agreement table;
  pipeline test with a facts file holding an excluded and a kept rule.
- **S0.2 FIX R2.** `extractors/webapp -skip-dirs`, merged into
  `skippedDir`; `scanwebapp.sh` passes one shared variable to both
  extractors.
- **S0.3 FIX R3.** `extractors/go`: `packageVars(pkgs, fset, root)`
  indexes every package-level `var` of non-test files by its name's
  position, citing the spec; `TestFacts` resolves test uses against
  functions, methods, types and these vars. `SymbolFacts`, call facts
  unchanged. Tests: a fixture test reading `Table.Field` gives a `test`
  fact on the var; a `const` gives none.

### S1. Part 1: `handler_lines` on GraphQL route facts

`attrHandlerLines`; `routeFact` sets `lines(m.start, m.end)`. Goldens
regenerated, diff = `handler_lines` only. Core test through the real path
(webapp extractor on its fixture → `graph.Build`): an endpoint's journey
lists a helper's cited rule.

### S2. Part 2 step 1: `access_check`, `checks`, `check_args`

New file `extractors/webapp/checkfacts.go` (authz.go is already 1,274
lines; nothing added to it but a call):

- `accessCheckFacts`: one `access_check` fact per known check definition
  of the service (marked checkers, access predicates, helper predicates)
  and of the shared packages it imports (`Root: pkg`), id
  `access_check.<pkg>.<Func>`, attrs `name`, `form`.
- On each guard's rule fact: `checks` (sorted ids of the checks its
  condition calls, or `open`/`todo`) and `check_args` (JSON list, one
  entry per check call in source order: `check`, and when present
  `capability` (Go name of a `capabilities.X` argument), `matcher` (an
  `acl.*Scope` argument's identity), `subject` (the argument of a self or
  coach check), `rate` (`count`, `window`, `key` of a rate limit)).
  `scope_of_check: item` on loop and visibility guards.
- Core: `extract.KindAccessCheck`; kind added to the exclusion list.
Tests: table over guard shapes on the fixture; goldens reviewed;
`TestExtractorRuleFactsPass` green. Measure: distinct checks per service,
rules whose checks all carry their operands.

### S3. Part 2 step 2: `unguarded` facts

`unguardedFacts`: a public resolver method (root or field) with no guard
and no marker → one fact citing the method. `extract.KindUnguarded`,
excluded like the rest. Measure open/todo/guarded/unguarded per service.

### S4. Part 2 design 3 tail: disagreement reads `check_args`

Rule facts' `check_args` reach the node as a frontmatter extra
(`frontmatterExtras`, copied by `ruleFactExtra`), so
`grounding.Annotate` treats a value named in `check_args` as carried by
the condition. Test: a rule whose text names `acl.GlobalScope` and whose
`check_args` has it records no disagreement. Measure the 298.

### S5. Part 2 step 3: calls through package-level vars (design 8)

A `var` initialized by a call or function literal becomes a call target;
its initializer's references are its calls; built on S0.3's index.
Measure on a scratch copy: call facts added, journey lengths, drift
`affected`, `callers` placements. Adopt only if the measurement is
clean; otherwise stop and report.

### S6. Part 2 step 4: `reads`, `uses_value`, `loads`, struct `tags`

Go extractor, one file each, every function: selector reads of module
struct fields; references to package-level consts and vars with declared
type and literal value (generated files included); datastore loads (kind
from `NewQuery(<Kind>)`/`Get`/`LoaderForQuery`, filter properties). Tags
on `struct` facts. Kinds added to the exclusion list. Measure facts size
and time on districts.

### S7. Part 2 steps 5-6: `internal/policy` and `codetospec policies`

Pure `policy.Build(facts []extract.Fact, rules []graph.Node, reg
Registry) Inventory`; kinds per design 11; registry YAML loader in the
shell. Command reads `graph.json`, nodes and `.codetospec/facts.json`,
writes md or json to stdout. Golden on the fixture; two runs identical.

### S8. Part 2 steps 7-8: validation on real outputs

On scratch copies: districts recall/precision against the manual pass,
then the other services. Recorded in `docs/verification/`.

### S9. Part 3

S9.1 capability and matcher facts; S9.2 `policy.Model`; S9.3
`internal/cerbos` + `--format cerbos`; S9.4 `internal/cedarpolicy`
(stops for the cedar-go dependency decision); S9.5 equivalence
measurement; S9.6 several graphs; S9.7 docs.

## Go-advice review of this plan

Applied before starting:

- **Functional core, imperative shell (§5).** Every new extractor
  function takes parsed trees and returns facts; `policy.Build` and the
  renderers are pure; files are read and written only by `main`/`run`
  and the CLI command. No new method mutates its receiver.
- **Deep modules (§4).** One entry point per new package
  (`policy.Build`, `cerbos.Export`); helpers unexported. No pass-through
  wrappers: `check_args` is computed where the guard is, not re-derived.
- **Comments first (§4, §13).** Each exported function gets its
  `Requires:`/`Ensures:` comment before its body; a tangled comment sends
  the design back.
- **No globals, no `init` (§1, §10).** Kind lists are constants or
  function results; configuration flows through parameters.
- **Tests (§9-10).** Stdlib only; table-driven, named cases, `t.Run`;
  tests through the exported API and the real paths AGENTS.md names
  (`sitter.Parse` → facts → `Merge`; extractor → graph); golden files
  regenerated with the existing `UPDATE_GOLDEN=1` convention, never a new
  flag. New packages' tests use the `_test` package suffix.
- **One concept per file, ≤ 1000 SLOC (§19).** New code goes in new
  files; `authz.go` gets only call sites.
- **Errors (§3).** Wrapped with what was being done (`policy: load
  registry: …`); command errors lowercase `<command>: <reason>`; nothing
  swallowed.
- **Consistency (§15).** Fact ids, attribute spellings and kinds follow
  the existing contract in `internal/extract`; the CLI follows the
  existing `config.Parse*` + `cli` method pattern, not a new framework.
- **Delete dead code (§15).** Anything a step makes unused goes in the
  same step.

## Progress

- **S0 done.** FIX R1 (`source.Excluded`, `extract.WithoutExcluded`,
  README count), R2 (webapp `-exclude-dirs`, named apart from the Go
  extractor's narrower `-skip-dirs`; `scanwebapp.sh` `test_helper_dirs`),
  R3 (`extractors/go/vars.go`; 50 admin rules gain tests,
  `docs/verification/testreach-package-vars-2026-11-06.md`).
- **S1 done.** `handler_lines`; every GraphQL endpoint of districts and
  users gets a journey; `exposed_by`, nature and questions don't move
  (`docs/verification/resolver-journeys-2026-11-06.md`).
- **S2 done.** `checkfacts.go`: `checks`, `check_args`, `access_check`.
  Design changed from the plan: check names, not fact ids, in
  `check_args` (consolidation renames ids per service); `ruleFacts` takes
  the known checks (five call sites) rather than annotating guards at
  construction (five builders plus literals would miss some).
- **S3 done.** `unguarded`; the first count (54) was 100 % cross-type
  delegations, now recognized as delegations (`delegates_to`); 0
  unguarded remain (`docs/verification/policy-checks-2026-11-06.md`).
- **S4 code done** (graph `checks`/`check_args`/`delegates_to` extras;
  grounding counts `check_args` values as stated); measurement running.
- **S4 done.** Extractor-rule disagreements 164 → 98 (districts),
  170 → 87 (users) (`docs/verification/check-args-disagreement-2026-11-06.md`).
- **S5 done.** A first version (every variable initialized by a call)
  was rejected by its measurement: sentinel errors became callees. Only
  function-typed variables are callable now; +111/+116 call facts, users'
  shared ACL helpers placed as shared
  (`docs/verification/calls-through-vars-2026-11-06.md`).
- **S6 done.** `reads`, `uses_value`, `loads`, struct `tags`; +9.5 %
  facts, +0.15 s on districts (`docs/verification/check-inputs-2026-11-06.md`).
- **S7 done.** `internal/policy` (`Build`, registry, Markdown) and
  `codetospec policies`. The end-to-end test found field-resolver guards
  dropped (no endpoint): rule facts now carry `operation`, the point when
  there is no endpoint. Registry methods deferred.
- **S8 done (districts).** Recall 6/6 against the manual pass's list;
  precision read on one check; closure noise from infrastructure
  packages noted (`docs/verification/policy-inventory-2026-11-06.md`).
  Other services: not run (the cross-service join is a script, step 8).
- **S9 done.** S9.1 capability facts (129 on admin, 48 with scope keys,
  shared key lists resolved); S9.2 `policy.BuildModel` (an operation with
  an untranslated guard is left out whole: the test caught a model that
  would have exported a bare allow); S9.3 `internal/cerbos` (real `cerbos
  compile` built from the checkout: 22/22 on admin, 296/296 on districts
  joined); S9.4 `internal/cedarpolicy` (cedar-go v1.9.0, parse + strict
  validation in `Export`, `cedar.Authorize` 22 cases, 0 disagree); S9.5
  `docs/verification/policy-export-2026-11-06.md`; S9.6
  `--capabilities-from`. Matcher facts dropped (templates pinned in the
  renderers' docs). Deferred: registry methods; pinning service-local
  checkers' and admin wrappers' meaning (R4), the largest remaining gap.
