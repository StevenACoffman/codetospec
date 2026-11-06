# Test reach through package-level variables (FIX.md R3)

Question: does making package-level variables targets of `test` facts give
admin's role and capability tables the tests that exercise them, without
false credit?

## Revisions and commands

- codetospec working tree on `ccc0566` (FIX.md R3: `extractors/go/vars.go`,
  `TestFacts` resolving uses against `packageVars`); webapp `c55b04b0090`.
- Old: `bin/go-extract` built for the 2026-11-05 remap. New: the extractor
  built from the working tree. Both run on `services/admin` with
  `-root . -skip-dirs testutil`.
- Rules: `~/spec-graphs/admin/nodes/rules/*.md` (the 2026-11-05 remap),
  matched to `test` facts by citation overlap, as `internal/testreach`
  does (direct reach only, depth 0).

## Results

- `test` facts: 1,543 → 1,934 (+391).
- Rules going from no test to at least one: 50, all in two domains:
  `roles` 26, `capabilities` 24. No other domain changed.
- The two invariants FIX.md F3 named are reached:
  `rolesSuite.TestImpersonatorOnlyGrantsImpersonation` reaches
  `permissions/roles/support.go:121-127` (`Impersonator`);
  `rolesSuite.TestKAISupporterIsStrictSubsetOfSupporter` reaches
  `support.go:100-116` (`KAISupporter`) and `:33-69` (`Supporter`).
- Sample read for false credit:
  - `capabilitiesSuite.TestGetAllCapabilities` lists capabilities and
    asserts they're a subset of `AllCapabilities`: a real test of
    registration rules.
  - `userResolverSuite.TestHasPermission*` pass capability variables into
    the permission checks they exercise: indirect, but the capability's
    behavior is what they assert.
  - No test in the sample names a variable only to pass it through
    unrelated code.

## Limits

- Direct reach only; `testreach` also follows call facts up to depth 3,
  which this count leaves out, so the real gain can only be larger.
- 20 of the 46 role rules still have no test: their roles appear in no
  test.
- One service. The other outputs gain only where tests read package-level
  tables.
