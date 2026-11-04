# Why the remap lost 11–19 % of the rules — 2026-11-04

## Question

The corpus baselines re-recorded after the remap (`e3e47d7`) count 11–19 %
fewer rules on every entry but assignments. Did the remap merge duplicates,
or lose rules?

## Revisions

- Old outputs: `~/spec-graphs.before-remap-2026-11-02` (pkg mapped
  2026-10-14 at codetospec `c9b009e`, webapp `1ec3acc`).
- New outputs: `~/spec-graphs` (remapped 2026-11-02 to 11-04 at
  codetospec `9a18cc5`, webapp `88b55ce5c33`, `--quality --crosscheck
  --acceptance`).
- Model `gemini-3.8-flash` in both (`scanwebapp.sh`'s default since
  `5b65a7b`; the state does not record it).

## Command

```sh
bin/codetospec diff --old ~/spec-graphs.before-remap-2026-11-02/pkg \
  --new ~/spec-graphs/pkg --format json
bin/codetospec diff --old ~/spec-graphs.before-remap-2026-11-02/graphql-gateway \
  --new ~/spec-graphs/graphql-gateway --format json
```

then disposable Python over both outputs: rules by origin (own LLM,
`shared-*` inherited, `extracted_by`), each removed rule's citations
against every new rule's (the `sources:` block only), and the map caches
(`.codetospec/map/*.json`).

## Result

Rules by origin, old → new:

| output | own LLM | inherited (`shared-*`) | extractor |
|---|---|---|---|
| pkg | 1,424 → 1,149 | 13 → 14 | 0 → 0 |
| admin | 233 → 202 | 719 → 575 | 86 → 86 |
| ai-guide | 4,771 → 4,245 | 826 → 662 | 256 → 249 |
| assignments | 1,061 → 935 | 12 → 603 | 200 → 205 |
| coaches | 1,273 → 1,143 | 777 → 632 | 233 → 233 |
| districts | 2,346 → 2,063 | 783 → 634 | 427 → 425 |
| graphql-gateway | 178 → 172 | 681 → 552 | 24 → 21 |
| users | 1,590 → 1,423 | 867 → 717 | 301 → 301 |

The inherited rules fall with pkg's spec (−19 %); assignments grows only
because it inherited nothing before.

pkg, the source of both drops:

- Same 764 chunks (ids equal), every one remapped (new prompt key). Map
  candidates 1,477 → 1,212; the reduce keeps ~96 % in both runs. The drop
  is the map's, not the reduce's.
- Candidates by nature: technical 786 → 550, business 662 → 633,
  presentation 29 → 29. 58 chunks that gave rules give none now (7 the
  reverse): SDK forwarding wrappers, metrics helpers, header getters, test
  helpers, a proto schema.
- specdiff: 1,171 removed, 897 added, 266 changed, 0 unchanged (every rule
  is reworded by the EARS-components prompt, `7324aae`, so few match).
- The 1,171 removed:

| | rules | technical | business | presentation |
|---|---:|---:|---:|---:|
| cited code overlaps a new rule | 801 | 373 | 412 | 16 |
| file still cited, those lines not | 276 | 206 | 65 | 5 |
| file no longer cited (35 files) | 94 | 87 | 7 | 0 |

  The 801 hit 692 distinct new rules; 93 of them absorb two or more
  removed rules.

graphql-gateway: 707 removed, 569 added, 176 changed, 0 unchanged; the net
loss is in `shared-*` (inherited) domains.

## Reading

- **Mostly lost, not merged.** 370 rules (25 % of pkg's) cite code no new
  rule cites; 79 % of them technical: error wrapping, pointer checks,
  cache encoding, context cancellation. Merging is real but smaller: 93
  new rules stand for two or more old ones.
- **Some lost rules are behaviour worth keeping**: gRPC error-code
  mapping (`InvalidArgument` → `InvalidInput`), secret-category
  validation, a feature-flag fallback, the `BaseModel` key lint.
- **The cause is the map prompt, not the code.** pkg's code barely moved
  (13 files), the model is the same; the prompt changed between
  `c9b009e` and `9a18cc5` (EARS components, structured conditions,
  vocabulary, actors), and nothing in it asks to skip plumbing. Which
  change cuts technical rules is not measured.
- specdiff under-reports renames on a full remap: rewording moves the
  requirement and often the citation, so most survivors show as removed
  plus added.

## Limits

- One output (pkg) analysed rule by rule; the services read by origin
  counts only.
- Overlap is any shared line, a loose notion of "same rule".
- 16 removed rules read, by requirement only.
- The map is not deterministic: some of the drop may be run-to-run
  variance; no repeat run measured it.
