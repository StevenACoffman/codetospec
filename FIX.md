# FIX — role → capability rules on admin

Findings from checking the admin service's role rules against their code
during the 2026-11-05 remap, what to change, and how. `TODO.md` points
here; once the fixes ship, their measurement goes into a new
`docs/verification/` file and this file can be deleted.

## Scope and method

- Output: `~/spec-graphs/admin` (remap of 2026-11-05, `scripts/remap.sh`
  → `scanwebapp.sh admin --quality --crosscheck --acceptance
  --write-domain-model`).
- Revisions: codetospec `d93e32c`, webapp `c55b04b0090` (2026-11-04).
- Code checked: `services/admin/permissions/roles/*.go` (one
  `RegisterRoleDefinition(RoleDefinition{...})` per role) and the code
  that reads them (`permissions/roles/helpers.go`,
  `permissions/scopes.go`, `permissions/management.go`), plus
  `permissions/roles/helpers_test.go`.
- Method: an independent `go/ast` parse of every non-test file in
  `permissions/roles/`. It doesn't share code with
  `extractors/webapp/roles.go`. Each role's name, description, capability
  list (in order) and definition span was compared with the extracted
  rule's sentence and citation. The three LLM rules of the `roles` domain
  were read against `helpers.go`. Each rule's frontmatter (`tests`,
  `nature_evidence`, edges) was read against the code and tests that
  should feed it.

## Findings

### F1. The 46 role rules are correct (no action)

- The code has 46 roles and the `roles` domain has 46 extracted rules.
  Every name appears on both sides.
- All 46 match exactly on description, capability list (same order) and
  citation. Each citation is the full `var X = RegisterRoleDefinition(...)`
  span.
- Every definition is a plain literal: no computed or shared capability
  sets. No role sets `GrantingCapability` (only a capability, `content.go:65`,
  does), so the sentence leaves nothing out.
- The three LLM rules of the domain are right, and the crosscheck marked
  each one `supported`:
  - an unknown name returns InvalidInput (`helpers.go:21-30`);
  - a duplicate registration panics (`helpers.go:44-55`);
  - `IsRoleNameValid` checks membership in the registry (`helpers.go:34-37`).
- Counts: `roles` holds 49 rules (46 extracted + 3 LLM). The largest role,
  `base_engineer`, grants 31 capabilities.

### F2. A test-helper role leaked into the spec (bug)

- `rule.testutil.role-admin-data-suite-can-view-user-permissions-role`
  cites `testutil/admin_data_suite.go:24-28`. It's a role that the
  `AdminDataSuite` test helper registers, and it has its own domain
  (`domain.testutil`) in the output.
- The run passed `--exclude ...,testutil,...` (`scanwebapp.sh:121`).
  `--exclude` only filters codetospec's own walk
  (`source.Walk`, `cmd/codetospec/pipeline.go:253`).
- The webapp extractor walks the service with its own fixed skip list:
  vendor, node_modules, generated, testdata and hidden directories
  (`extractors/webapp/analyze.go:428`).
- `pipeline.extract` merges extractor facts and validates the rule facts
  (`pipeline.go:270`) without checking their citations against
  `--exclude`. So any rule an extractor states about excluded code is kept,
  and so is the domain it creates.
- `scanwebapp.sh:188` already passes `-skip-dirs testutil` to the Go
  extractor, but nothing equivalent reaches the webapp extractor. The
  user's exclusion is honoured in one place and missed in another.

### F3. `tests: 0` on every role rule is wrong (bug)

- `permissions/roles/helpers_test.go` asserts role contents directly:
  - `TestImpersonatorOnlyGrantsImpersonation` checks that `Impersonator`
    grants exactly `CanImpersonateUsers` (SEC-1014);
  - `TestKAISupporterIsStrictSubsetOfSupporter` checks that `KAISupporter`
    is a strict subset of `Supporter`;
  - a registration test names several role variables (`AssignContentPeopleRole`,
    `DevAdminTechnical`, `BaseEngineer`, ...).
- Yet `roles.role-impersonator`, `roles.role-kai-supporter` and the other
  44 all say `tests: 0`. That reads as known-untested, which is worse than
  unknown.
- Cause: `extractors/go/symbols.go:48-60` (`addFileDecls`) skips every
  `*ast.ValueSpec` ("variables and constants are skipped: a rule is about
  behavior"). `TestFacts` resolves what a test names through
  `declsByPosition(moduleDecls(...))` (`extractors/go/tests.go:26-28`), so
  a test that names a package-level `var` produces no `test` fact.
  `internal/testreach.testsCiting` matches rules to reached definitions by
  span overlap. The role rule cites only the var's span, so nothing
  overlaps it.
- The same blind spot affects every rule that cites only a package-level
  variable: tables, registries, sentinel errors, option sets.

### F4. The role sentence leaves out the grant's scope (wording gap)

- "The system shall grant the `X` role ... the capabilities ..." is right,
  but in the code a role grant gives each capability at that grant's scope
  (`permissions/scopes.go:169-185`), not globally.
- That behaviour is stated by
  `permissions.uncached-capability-scopes-aggregation`, cited at
  `scopes.go:161-186`, but the role rules don't link to it, so someone
  reading a role rule alone would assume a global grant.

### F5. Unregistered roles are skipped silently, and no rule says so (missing rule)

- In `scopes.go:171-177`, when a stored role grant names a role that's no
  longer registered, the code logs a warning and skips it. Capability
  checks keep working and the grant confers nothing.
- `management.go:224-227`, by contrast, returns the error when roles are
  being added.
- Searching the rules that cite `scopes.go` found none that states the
  skip. The aggregation rule (F4) states only the matching cases. A
  deleted role quietly revoking access is a security-relevant behaviour
  and should be a rule.

### F6. Two security invariants exist only in tests (accepted, record by hand)

- "kai_supporter grants a strict subset of supporter" and "impersonator
  grants only impersonation" are stated only by tests. The code just lists
  two capability sets.
- The pipeline is right not to credit behaviour that only a test shows
  (crosscheck prompt; `docs/verification/crosscheck-tests-evidence-2026-11-01.md`),
  so they appear only as the two concrete role tables.

### F7. An unimplemented doc claim is correctly absent (no action)

- `permissions/roles/role.go` says a user must also "qualify" for every
  capability in a role. The code doesn't do that (it's marked a partial
  port, with a TODO). No rule claims it.

### F8. Role rules carry no nature evidence and no concept link (observation)

- The role rules have `nature: business`, `facets: access`,
  `quality_attribute: security`, and no `nature_evidence`. They have no
  `touches` edge: only reduced rules get one (`graph.reducedRuleNode`,
  `graph.go:428`), and rule facts can't declare one. They have no
  structured condition, and security doesn't count as technical evidence.
- About 107 of admin's rules are business by text with no evidence at all.
  Most of them are probably role tables like these.
- Conceptually the role tables define what a grant stored in
  `UserPermissions` means, but nothing structural links them to that
  concept, so `concept.Assign` can't place them once a domain model is
  confirmed.

## Recommendations

| # | Finding | Change | Priority |
|---|---|---|---|
| R1 | F2 | Apply `--exclude` to the citations of extractor `rule` and `domain` facts in the core | High |
| R2 | F2 | Pass the same exclusions to the webapp extractor in `scanwebapp.sh` | Medium |
| R3 | F3 | Index package-level `var`s as test targets in the Go extractor | High |
| R4 | F4 | Say "at the scope of the grant" in the role sentence | Low |
| R5 | F5, F6 | Record the missing rule and the two invariants in a person-owned rule-fact file | Low |
| R6 | F8 | Let a rule fact name the entity it governs; decide after the concept measurement | Deferred |

### R1. Exclusions apply to extractor rule facts

Make `--exclude` mean the same thing for every layer: if a rule fact or
`domain` fact cites a path under `--src` that the walk would skip, drop it,
along with the domain it would create. Do it in the core, so every
extractor (webapp, PHP, any future one) gets the fix without needing its
own flags.

### R2. One exclusion list for every extractor in the scripts

Pass `scanwebapp.sh`'s exclusions to the webapp extractor too, as it
already does for the Go extractor (`-skip-dirs testutil`). With R1 this is
defence in depth, and it saves the extractor from analysing test helpers.

### R3. Tests that name a package-level variable reach it

Index package-level variables as test-use targets, so that
`Impersonator.CapabilitySet` in a test produces a `test` fact for the
`Impersonator` var's span. Keep `SymbolFacts` and the call facts unchanged
(map anchors, repair catalog and drift callees stay as measured), and leave
constants out: a test that compares against a constant asserts the code
under test, not the constant.

### R4. The role sentence names the grant's scope

Change the webapp role template to "... the capabilities ..., each at
the scope of the grant". Wait for R1–R3 to land and be measured first. It
changes all 46 rule texts, so keeping it separate keeps that diff readable.

### R5. Record what the pipeline can't derive

For F5, the unregistered-role skip, and for the two F6 invariants, write
rule facts in a person-owned facts file for admin (for example
`~/spec-graphs/admin/adopted.facts.json`), passed with `--facts`.
`graph.ValidateRuleFacts` checks them, and they render with
`extracted_by` set to the file's origin. The only code change is in
`scanwebapp.sh`, which passes fixed `--facts` paths today and needs to
pass this file when it's present.

Hand-reviewed rule files (`status: reviewed`) can't do this:
`render.KeepReviewed` keeps a person's file only in place of a rule the
run produces under the same id, and drops any other
(`internal/render/reviewed.go:167-196`). The pipeline never produces
these three rules, so their files would be dropped on the next run.
`resolutions.yaml` only answers questions; none of the three is a
question.

If F5 recurs across services, consider a map prompt note on "error paths
that skip and continue", measured as usual.

### R6. Rule facts may name the entity they govern (deferred)

Let a rule fact carry an entities attribute (a contract constant in
`internal/extract`), validated against entity nodes and rendered as a
`touches` edge. The webapp role rules would then name the permissions
entity. It's deferred because a role table doesn't read or write
`UserPermissions`: "touches" may be the wrong edge, and a concept-only edge
needs the measurement that `TODO.md` "Apply the concept regrouping" already
asks for. Revisit after that.

## Implementation plan

Ground rules for all steps (`AGENTS.md`):

- Rebuild `bin/codetospec` and the extractors after every code change
  before running them. The 2026-11-05 remap has finished, so nothing
  running depends on the old binaries.
- Branch first. Use scope-first commits (`graph: ...`, `go-extract: ...`).
- `make check` must pass. Remember that the extractors are separate modules.

### Step 1 — R1: exclude extractor rule and domain facts by citation

1. `internal/source/walker.go`: export the walk's notion of an excluded
   path. Add `Excluded(rel string, exclude []string) bool`. It returns true
   when any slash-separated element of `rel` is hidden (`.` prefix) or
   matches `excluder(exclude)`, so it decides exactly as `Walk` does.
   `Walk` keeps using `excluder`. Add a table test in `walker_test.go` that
   checks the two agree on a temporary tree: walk it, then assert that
   `Excluded` is false for every walked file and true for a file under each
   excluded directory and glob.
2. `cmd/codetospec/pipeline.go`, `extract`: after `extract.Merge(...)` and
   before `graph.ValidateRuleFacts`, drop facts of kind `extract.KindRule`
   and `extract.KindDomain` (what `extract.DomainHints` reads) whose
   `Source.Root == ""` and whose `Source.Path` is `Excluded` under
   `p.cfg.Exclude`. Inherited rules cite another root and are untouched.
   Emit one `ui.LogLine` (warn) per dropped fact, the way rejected rule
   facts are reported, and count them in the final state next to
   `RuleFactsRejected` (e.g. `RuleFactsExcluded`).
3. Audit the other node-producing extractor kinds (route, enum, table,
   entity, `api_type`) for the same leak. List which ones cite a path and
   whether a node from an excluded file is ever wanted. Extend the filter
   only where the answer is clearly "never", and note the decision in the
   step's commit message.
4. Tests in `cmd/codetospec`: run the real pipeline path on a fixture
   (`testdata/fixture` or a temp tree) with a facts file holding a rule
   fact under an excluded directory and one outside it. Assert that the
   first is absent from the graph (no rule, no domain) and the second is
   present.
5. `AGENTS.md`: one sentence under "`rule` and `domain` facts" saying
   `--exclude` applies to their citations. `README.md`: the same in the
   options section.

### Step 2 — R2: webapp extractor honours the scripts' exclusions

1. `extractors/webapp`: add a `-skip-dirs` flag (comma-separated directory
   names, same meaning as the Go extractor's), added to `skippedDir`'s
   fixed list. Test it from `extractors/webapp` with a fixture directory
   that holds a role definition under a skipped directory.
2. `scanwebapp.sh:171`: pass `-skip-dirs testutil`, kept next to the Go
   extractor's at line 188, preferably from one shell variable so the two
   can't drift.
3. Regenerate the webapp goldens only if they change
   (`UPDATE_GOLDEN=1`, from `extractors/webapp`), and review the diff.

### Step 3 — R3: package-level variables as test targets

1. `extractors/go/tests.go`: build the test-use index from a wider set
   than `moduleDecls`. Add `testTargets(pkgs, fset, root)`, which returns
   `moduleDecls` plus each package-level `var` spec of non-test files,
   positioned at its name. A spec with several names gets one entry per
   name, all citing the spec's span. Use it in `TestFacts` in place of
   `moduleDecls` for `declsByPosition`. Leave `moduleDecls`,
   `addFileDecls`, `SymbolFacts` and the call facts unchanged, and update
   `addFileDecls`' comment to say why vars are skipped there but not for
   tests.
2. Leave out `const` specs (finding rationale above) and blank `_`
   variables. Local variables are never at package level, so they're out
   by construction.
3. Unit test (`extractors/go/tests_test.go`): a fixture package with
   `var Table = register(...)` and a test reading `Table.Field`. Assert a
   `test` fact whose callee span is the var's spec. Add a negative case: a
   test that compares against a `const` produces no fact for it.
4. Pipeline test through the real path (`AGENTS.md` pitfall: no
   hand-built facts): Go extractor → `testreach.Annotate`. A rule citing
   the var's span gets `tests: 1` and `tested_by` naming the test.
5. Measure before adopting: run the Go extractor on admin and pkg before
   and after, then count rules whose `tests` goes from 0 to more than 0,
   by domain. Hand-check a sample for false credit (a test that names a
   var only to pass it along, not to assert on it). Record this in
   `docs/verification/testreach-package-vars-<date>.md`. Expected on admin:
   `roles.role-impersonator`, `roles.role-kai-supporter` and the
   registration test's roles become tested.
6. `AGENTS.md`, `internal/testreach` paragraph: add that package-level vars
   are test targets, and that constants aren't.

### Step 4 — re-measure and re-record

1. Rebuild every binary: `go build -o bin/codetospec ./cmd/codetospec`,
   then the extractors through the scripts.
2. Re-run admin from its cache (`scanwebapp.sh admin` with the same
   flags). Map, reduce, crosscheck and acceptance caches stay warm:
   neither change touches a prompt or a chunk. Expect:
   - no `domain.testutil`, and the testutil role rule gone;
   - `roles` rules with `tests` > 0 where `helpers_test.go` names them;
   - no other rule text changed (diff `nodes/rules/` apart from `tests`,
     `test_depth`, `tested_by`).
3. `make corpus`. The corpus doesn't count `tests`, so step 3 moves
   nothing in it. Step 1 does: admin loses its leaked `testutil` rule.
   Review the diff, then re-record with `CODETOSPEC_CORPUS_UPDATE=1`.
   The remap's re-record of 2026-11-05 is already done.
4. Write the measurement file (step 3.5). Then tick the `TODO.md` entry
   and delete this file, or keep only R4–R6 in it.

### Step 5 — R4: role sentence names the grant's scope (after step 4)

1. `extractors/webapp/roles.go` `roleFact`, plus the template's `locale`
   strings in the extractor's wording, in FR and EN: append the scope
   clause.
2. Regenerate the goldens. `TestExtractorRuleFactsPass` must still accept
   the sentence (`ears.Check`).
3. Re-run admin. All 46 role rules change text but keep their ids (same
   citations, so `graph.MatchRules` keeps them). Check with
   `codetospec diff --old --new`: 46 `reworded`, nothing else.

### Step 6 — R5: hand-recorded rules (any time)

1. `scanwebapp.sh`: pass `$out_dir/adopted.facts.json` with `--facts`
   when the file exists. (Done 2026-11-06.)
2. In `~/spec-graphs/admin/adopted.facts.json`, write rule facts for the
   F5 rule (the unregistered role skipped with a warning, citing
   `permissions/scopes.go:169-178`) and the two F6 invariants (citing the
   two role definitions each compares), with the attributes
   `graph.ValidateRuleFacts` requires.
3. Re-run admin from its cache and check that the three rules appear,
   with `extracted_by` naming the file's origin.

### Step 7 — R6 (deferred)

Revisit after the `TODO.md` item "Apply the concept regrouping" has its
measurement on a confirmed model. At that point, decide between a
`touches` edge from rule facts and a concept-only hint, and plan it then.
