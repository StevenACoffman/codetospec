# Go struct fields in the condition vocabulary — 2026-11-02

## Question

On districts-jobs, 66 of 69 condition variables were coined under
`local.` (`conditions-acceptance-2026-11-02.md`): those modules have no
tables, no domain model, and no extractor rule conditions, so the
vocabulary offered almost nothing and equal checks could not hash equal
across rules. Does offering the Go extractor's struct fields change that,
and does the map use them correctly?

## Revisions

- codetospec: branch `conditions-acceptance` (`a3cea84`) plus the
  uncommitted struct facts: `extractors/go/structs.go` (`struct` facts,
  module types outside generated files, and, with `-shared`, the shared
  types the module reaches) and the vocabulary's owner gating (a field is
  offered only where its struct's name, or that name's last camelCase
  part, is a word of the chunk).
- districts-jobs: `9a064d61c`, modules `rosterjob-updates`, `khanxadmin`,
  `moa-transitions`.
- Model: `gemini-3.8-flash`, 4 workers. Outputs in a scratch directory
  (`~/spec-graphs` untouched), each module's map cache purged before the
  run (the vocabulary is outside `promptKey`), so every chunk was mapped
  again; reduce and acceptance followed.

## Command

```sh
rm -rf <scratch>/measure/<module>/.codetospec/map
OUT_ROOT=<scratch>/measure ./scandistricts.sh <module> --acceptance
```

A disposable script read each rule file's `condition` and counted, per
rule, its distinct variables, those under `local.`, the variables named
by two rules or more, and the `condition_hash` values two rules share.

## Result

Struct facts: rosterjob-updates 14 structs (48 fields; 9 shared from
`pkg/`), khanxadmin 10 (22; 9 shared), moa-transitions 11 (52; none
shared).

| | rosterjob-updates | khanxadmin | moa-transitions | total |
|---|---:|---:|---:|---:|
| rules / with a condition, before | 30 / 19 | 12 / 9 | 24 / 17 | 66 / 45 |
| rules / with a condition, after | 32 / 18 | 12 / 9 | 29 / 15 | 73 / 42 |
| distinct variables per rule, coined / all, before | 29 / 29 | 12 / 12 | 21 / 23 | 62 / 64 (97 %) |
| the same, after | 15 / 30 | 12 / 12 | 12 / 24 | 39 / 66 (59 %) |
| variables named by 2+ rules, before → after | 3 → 7 | 1 → 1 | 3 → 5 | 7 → 13 |
| condition hashes shared by 2+ rules, before → after | 1 → 2 | 0 → 0 | 1 → 0 | 2 → 2 |

An intermediate run of moa-transitions with module structs only (the
shared ones not yet emitted; moa-transitions reaches none) gave 29 of 42
coined: its gain comes from its own types.

Variables taken from the vocabulary: `server.Listener.IsActive` (4
rules), `moa.Config.DryRun` (3), `v1.RosterJob.Status` (2),
`moa.Config.MaxTransitions` (2), `server.Listener.Email` (2),
`moa.snapshot.state` (2), and one rule each for `v1.Filters.
DistrictSearchString`, `v1.RosterJob.DistrictId`, `DistrictName`,
`server.StatusUpdate.MsgTimestamp`, both `GoogleClientID`s,
`moa.holderRow.ExpirationDate`, `moa.ledgerStateRow.ObservedAt`,
`moa.watermark.LastRun`, `moa.window.closedOn`. khanxadmin used none
of its structs' fields.

Read by hand (the 21 rules naming a vocabulary variable): 20 name the
field the cited code tests. One does not:
`ui.redirection-to-login-for-unauthenticated` tests a missing email
*cookie* as `not exists server.Listener.Email` (the chunk mentions a
listener's email elsewhere). One condition compares
`moa.holderRow.ExpirationDate` with the string `"nil"` instead of
`not exists`: a notation error of the model, not of the vocabulary, that
grounding cannot catch (`nil` occurs in the code).

## Decision

Keep the struct facts and the owner gating. The coined share falls from
97 % to 59 %, and cross-rule names nearly double; the cross-rule hash
matches do not move yet, as two rules rarely check exactly the same
thing in one module. The remaining coined variables are mostly locals
(`local.err`, `local.isUUID`, `local.transitionsCount`), which no
declaration names.

## Limits

- Three small modules, one run each, one model; the map is not
  deterministic, so rule counts moved between runs (66 → 73) and part of
  the change is noise.
- Precision read on 21 rules only.
- The owner gating misses a field reached through a variable named
  neither after its type nor its last part (`r := q.Next(); r.Status`).
- Not measured on webapp (its extractor conditions are a richer source)
  nor on a module with a domain model.
