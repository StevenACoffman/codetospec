# Concept proposal from code seeds — 2026-11-04

## Question

`DOMAIN_PLAN.md` recommendations 1, 2, 7 and 10: propose a draft modelith
model of the business concepts from code evidence (owned federated types,
tables, foreign keys), never one model per current domain. The draft the
plan recorded for assignments (next step 2) was lost. Does the
mechanical proposal recover its seeds?

## Revisions

- codetospec `2141826` plus this change (api_type facts, foreign keys,
  `domainmodel.Enhance` writing `generated/concepts.modelith.yaml`).
- webapp `bfad3957bf1` (`services/assignments`; its migrations did not
  change since `88b55ce5c33`); rules from `~/spec-graphs/assignments`
  (read only).

## Command

```sh
(cd ~/khan/webapp/services/assignments && bin/go-extract -root . -skip-dirs testutil) > go.json
bin/webapp-extract -root ~/khan/webapp/services/assignments -lang en > web.json
```

then a disposable Go test (deleted after the run): `render.LoadDir` of
the output, the facts of both files, `tablesOf` / `apiTypesOf`,
`domainmodel.Enhance` with no human model.

## Result

- Foreign keys: the 10 of the plan's finding 8, all NOT NULL to a single
  parent: 6 tables → `assignments`, 4 → `assignment_students`.
- Proposed model: 28 entities, lint clean, all with a placeholder
  definition. `Assignments` owns its 6 config tables and
  `AssignmentStudents`; `AssignmentStudents` owns the 4 per-student
  tables — the one SQL aggregate the plan's draft recorded.
- API types: `Assignment @key` is folded into the `assignments` table;
  the six platform assignment types and their student-data types are
  proposed as concepts of their own; `Classroom`, `User`, `StudentList`,
  the progress types and `HomepageNavigationClass` are named as
  references owned elsewhere.
- Datastore kinds (webapp `table` facts: `ContentAssignmentChangeEvent`,
  `SubjectMasteryAssignment`, …) are proposed beside the SQL tables.

## Reading

- Ownership needs no word list: NOT NULL foreign keys give the aggregate
  exactly as the plan read it by hand.
- What the code cannot settle stays for a person: the taxonomy (the
  platform types are kinds of Assignment, finding 4) and the Datastore
  models the plan judged vestigial are not marked; every definition is a
  placeholder asking what the concept is and is not.

## Limits

- One service. `ALTER TABLE` foreign keys are not read (none in
  assignments).
- The proposal is not yet driving anything: concept assignment (the next
  step) reads a person's confirmed model, not this draft.
