# Rule ids across a domain move — 2026-11-04

## Question

`DOMAIN_PLAN.md` recommendation 25: regrouping rules into concepts must
not churn their ids (finding 9: `MatchRules` matched only within one
domain, so a moved rule lost its id, its caches, its review and its
`targets.yaml` and resolution entries). With matching across domains when
the cited code overlaps, how many rules of the last remap keep an id they
lost, and are those real matches?

## Revisions

- codetospec `2141826` (before) and `2141826` plus this change (after).
- Outputs `~/spec-graphs.before-remap-2026-11-02/<o>` (old) and
  `~/spec-graphs/<o>` (new), for pkg, assignments, districts.

## Command

```sh
codetospec diff --old ~/spec-graphs.before-remap-2026-11-02/<o> \
  --new ~/spec-graphs/<o> --format json
```

with each binary; changes counted by kind.

## Result

| output | before | after |
|---|---|---|
| pkg | added 897, changed 266, removed 1,171 | same |
| assignments | added 1,267, changed 476, removed 797 | added 1,265, changed 476, removed 795, renamed 2 |
| districts | added 2,152, changed 970, removed 2,586 | same |

Both renames, read side by side, are one rule moved from `classroom` to
`content-assignment` (assignment status → GraphQL mapping) and to
`resolvers` (filter mastery assignments by student).

## Reading

- The remap kept the domain strategy, so almost no rule moved domain: the
  change is for the concept regrouping to come, not for this remap.
- specdiff and the build share `MatchRules`, so the build keeps these two
  ids on a rerun the same way.
- Reading a domain from a rule id is now wrong for a moved rule; the
  README duplicate counts, `llms.txt` and inherited rules' `shared-*`
  domain read the `belongs_to` edge.

## Limits

- No output with a real regrouping exists yet; the cross-domain cases are
  pinned by `TestMatchRules`, not measured on a regrouped output.
