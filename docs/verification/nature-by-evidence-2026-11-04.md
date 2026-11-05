# Nature by structural evidence in the core — 2026-11-04

## Question

`DOMAIN_PLAN.md` recommendation 4 asks for each rule's subject matter /
technical split from evidence before any concept grouping.
`scripts/domainsplit.py` measured it with business and technical word
lists (`domain-split-2026-11-03.md`); the core decided on 2026-11-04 to
use structural evidence only (no hand-made English lists). How far does
the core's `nature_by_evidence` agree with the script, and what does
dropping the word lists cost?

## Revisions

- codetospec `2141826` plus this change.
- `~/spec-graphs/assignments` (1,743 rules) and `~/spec-graphs/coaches`
  (2,008), read only (`render.LoadDir`); webapp `88b55ce5c33` for the
  script's vocabulary.

## Command

1. `uv run scripts/domainsplit.py ~/spec-graphs/<o> ~/khan/webapp/services/<o> <dir>`
2. A disposable Go test (deleted after the run): `render.LoadDir`, then
   `position.Annotate` with no layer mapping and no domain model.
3. Disposable Python joining both by rule id.

## Result

| script verdict → core | assignments | coaches |
|---|---:|---:|
| subject → business | 775 | 927 |
| technical → technical | 514 | 574 |
| subject → technical | 70 | 66 |
| technical → business | 10 | 12 |
| subject → none (no structural evidence) | 250 | 233 |
| subject or technical → open | 51 | 67 |
| question → any | 73 | 126 |

Where both decide (business or technical on both sides), they agree on
1,289 of 1,369 rules in assignments (94 %) and 1,501 of 1,579 in coaches
(95 %).

Access facet: 237 of the script's 282 access rules in assignments carry
it (257 of 346 in coaches); 92 (87) more carry it in the core only.

## Reading

- The structural evidence reproduces the script's split where it
  decides; what the word lists bought is the 250 (233) rules the core
  leaves without a verdict: service code whose only business signal is a
  word of its text. Rule ids, not text, are what concept grouping needs,
  so those rules wait for a concept's evidence (step 5 of the
  implementation) rather than a verdict.
- The main disagreement, subject → technical (70 / 66), is service code
  with a reliability or performance attribute and no entity or endpoint
  edge, read by requirement: mostly "IF retrieving X fails, THEN …"
  inside a business flow — boundary handling (recommendation 6), which
  the script also called technical when it could read the trigger.
- The access facet differs both ways: the core counts every security
  rule with a structured condition (the script needed an access word),
  and misses the script's security rules with access words but no
  condition.

## Limits

- Two services, one run each; no hand check of the 70 beyond reading
  five requirements.
- The script itself was hand-checked at 37 of 40; agreement with it is
  not accuracy.
- No layer mapping or domain model given: with them, condition variables
  on model entities add business evidence.
