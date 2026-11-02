# Quality pass on business rules — 2026-11-01

## Question

`--quality` asked only about technical rules, so no authorization rule —
`business` by the map prompt, and by the webapp extractor's role rules —
could carry `security`. Asked about every rule but the presentation ones,
how many business rules get an attribute, and do the extractor's
authorization rules get `security`?

## Revisions

- codetospec: `81a5f55` plus the uncommitted change of the same day
  (`quality.classifiedRules`: every rule but `presentation`; the prompt
  says a rule can be functional and serve a quality at once).
- Graph: `~/spec-graphs/districts` (read only): 2,791 business, 682
  technical and 83 presentation rules; 439 of the business rules are the
  webapp extractor's (`extracted_by`), all authorization rules.
- Model: `gemini-3.8-flash`, 4 workers, batches of 40, a fresh cache.

## Command

A disposable test (deleted after the run) loaded the graph with
`render.LoadDir`, ran `quality.Classifier.Run` on it and counted the
attributes by nature and origin. 1,387 s wall time; no batch failed.

## Result

| nature | origin | `security` | other attribute | none (`""`) |
|---|---|---:|---:|---:|
| business | extractor (authorization) | 438 | 0 | 1 |
| business | LLM | 351 | 293 | 1,708 |
| technical | LLM | 74 | 322 | 286 |
| presentation | — | not asked | | |

The other attributes of LLM business rules: reliability 105, compliance
56, usability 48, performance_efficiency 36, compatibility 21,
maintainability 16, scalability 6, portability 3, availability 1,
safety 1.

Before the change, no business rule had an attribute (they were not
asked); 789 now carry `security`, 438 of the 439 extractor authorization
rules among them. Technical rules with an attribute other than
`functional_suitability`: 396 — the NFRs `export` now writes.

## Limits

- One run, one model: no repeat to measure how stable the answers are.
- The technical rules' counts are with the new prompt wording; the old
  prompt's answers on them were not re-measured.
- Correctness of each `security` answer on LLM rules not reviewed by hand;
  the extractor's rules are authorization by construction.
