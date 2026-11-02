# Positional nature against the LLM's — 2026-11-01

## Question

A rule's `nature` (business, presentation, technical) comes from the map's
reading of its text. Where its cited code lives says something too: code
in an adapter or infrastructure package is technical by position. How
often do the two disagree, and which one is right when they do — before
deciding whether either should win?

## Revisions

- codetospec: `81a5f55` plus the uncommitted `internal/position` of the
  same day.
- Graph: `~/spec-graphs/districts` (read only), 3,556 rules. Citations of
  another root (`@pkg`) are not placed: the mapping covers `--src` only.

## Command

A disposable test (deleted after the run) loaded the graph with
`render.LoadDir`, built two mappings with `position.Parse` and counted, for
each rule the mapping places (`Map.Of` ≠ ""), whether the positional
nature agrees with the rule's own; a second test printed four rules of
each kind of disagreement.

- Mapping A: technical = `handlers/... cmd/... gcloud/... external/...
  lib/... web/... graphql/... middleware/...`; business = `rostering/...
  permissions/... models/...`.
- Mapping B: A, plus `resolvers/...` technical (the GraphQL adapter layer).

## Result

| mapping | placed | agree | technical by position, business by text | business / technical | business / presentation | technical / presentation |
|---|---:|---:|---:|---:|---:|---:|
| A | 1,180 | 926 (78 %) | 164 | 56 | 30 | 4 |
| B | 2,121 | 954 (45 %) | 1,063 | 56 | 30 | 18 |

Sampled disagreements of A (16 rules read by hand): the text was the
better reading in most. `models/` holds technical helpers (deep copy,
pre-save timestamps, key formatting: technical by text, business by
position) and audit-log descriptions (presentation by text); `cmd/`
holds input validation of backfills (business by text). Position was the
better reading in a few `cmd/` rules about CLI option values.

Mapping B shows the limit: webapp's resolvers carry most of the business
logic, so calling the adapter layer technical contradicts 1,063 rules.

## Decision

Neither wins. A disagreement is recorded on the rule
(`nature_by_position`) and asked as a `nature_mismatch` question; the
rule's nature is never changed. A layer says where code lives, not what a
rule is about, and the mapping is only as good as the person who writes
it: with A, about one placed rule in five would be asked about.

## Limits

- One service, two hand-written mappings, 16 rules read.
- `nature_by_layer` (anarch.json) not measured: no Khan service has an
  anarch.json yet.
