# Positional nature backed by structural evidence — 2026-11-04

## Question

`nature_mismatch` asked about every rule whose layer contradicts its
nature: 226 on districts with mapping A
(`post-remap-measurements-2026-11-04.md`), most of them in `cmd/` and
`handlers/`, where the mapping, not the rule, is wrong. `TODO.md` folds it
into `DOMAIN_PLAN.md` recommendation 4: position becomes one signal beside
the rule's own evidence. Which evidence can the core use without word
lists (English, hand-made, Khan nouns), and how many questions remain when
only evidence backing the position asks?

## Revisions

- codetospec `b1b1192` plus this change.
- `~/spec-graphs/districts` as remapped on 2026-11-03 (3,122 rules),
  webapp `88b55ce5c33`.
- Mapping A: technical `handlers/... cmd/... gcloud/... external/...
  lib/... web/... graphql/... middleware/...`, business `rostering/...
  permissions/... models/...`.

## Command

1. Before the change, disposable Python over the rule frontmatter (the
   `sources:` block only: `callees:` also lists paths), with the
   structural signals: `touches`, `exposed_by`, a `models.*` condition
   variable, a technical quality attribute.
2. After it, a disposable Go test (deleted after the run): `render.LoadDir`,
   `position.Annotate` with mapping A and no domain model, then
   `questions.List`.

## Result

Before (1), the 192 business / technical disagreements (the 34 with a
presentation side aside):

| evidence sides with | rules |
|---|---:|
| the text | 66 |
| the position | 33 |
| both (conflict) | 6 |
| nothing | 87 (68 in `cmd/`) |

After (2), as shipped (business evidence counts against a presentation
text too; a condition variable counts when its owner is an entity node's
table, not by a `models.` prefix):

| layer | nature | placed | disagree | asked |
|---|---|---:|---:|---:|
| `cmd/...` | technical | 128 | 113 | 11 |
| `handlers/...` | technical | 35 | 33 | 0 |
| `models/...` | business | 226 | 59 | 32 |
| `permissions/...` | business | 43 | 0 | 0 |
| `rostering/...` | business | 593 | 21 | 6 |
| other technical layers | technical | 0 | 0 | 0 |

49 `nature_mismatch` questions, from 226. Every fifth (10), read by
title and evidence only, not the rule bodies: 8 in `models/` and 1 in
`rostering/` touch the entity they work on (`created_at` initialisation,
a unique key, field decryption, audit-log descriptions), business by
position and evidence, technical or presentation by text; 1 `cmd/`
backfill has a reliability quality attribute and is business by text.
Each sets two kinds of signal against each other; the 2026-11-01 reading
(the text was better for `models/` helpers) says the answer will often
be the text.

## Reading

- The questions left are where position has a second signal behind it.
  `handlers/` (94 % disagreement) and `cmd/` (88 %) show in the README
  table, not as 146 questions: their mapping is wrong.
- `touches` dominates the business evidence: a technical helper of a model
  touches its entity. Those are asked, which is the intent (a second
  signal backs the layer), but the touched-entity signal says what a rule
  works on, not what it is about.

## Limits

- One service, one hand-written mapping; 10 questions read by title and
  evidence, by the author.
- `touches` and `exposed_by` come from the LLM reduce and the handler
  overlap: evidence, not ground truth.
- No presentation evidence exists: a presentation layer is never backed,
  so never asked.
