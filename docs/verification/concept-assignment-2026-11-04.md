# Concept assignment from evidence — 2026-11-04

## Question

`DOMAIN_PLAN.md` next step 3 (recommendations 3, 12, 13, 15): place each
business rule in a concept of a confirmed model from evidence, and
measure coverage, conflicts and the size of the regrouping before any
domain moves. No person-corrected model exists yet; how does assignment
behave on the code's own proposal taken as confirmed?

## Revisions

- codetospec `2141826` plus this change.
- `~/spec-graphs/assignments` (1,743 rules), read only; the model is the
  proposal of `concept-proposal-2026-11-04.md` (28 entities, owned
  relationships from the 10 foreign keys), unedited.

## Command

A disposable Go test (deleted after the run): `render.LoadDir`,
`position.Annotate` (no layer mapping, the model's entities),
`domainmodel.Compare`, `domainmodel.Concepts`, `concept.Assign`.

## Result

- 794 rules business by evidence; 15 of the 28 concepts matched a code
  entity node (the others are API types and tables no rule touches).
- 625 business rules placed (79 %), 11 conflicts, 158 with no concept
  evidence.
- `Assignments` takes 494 rules from about 20 current domains
  (`content-assignments` 90, `read` 59, `all-assignments` 54,
  `platform-assignment` 51, `content-assignment` 50, …);
  `AssignmentStudents` 42; the other 12 concepts 2–20 each.
- Every conflict read pairs `Assignments` with a Datastore change-event
  kind (`CourseMasteryAssignmentChangeEvent`, …) that the proposal does
  not relate to it.

## Reading

- The regrouping is large and one-sided, as finding 4 said: one
  aggregate holds most of the service's subject matter, scattered over
  the layer domains today. This is the size recommendation 27 asks to
  bring to an expert before moving anything.
- Conflicts are model gaps, not rule errors: a person relating the
  change events to their assignment (or declaring them cruft) settles
  all 11.
- `content-assignment` and `content-assignments` (finding 1) both land
  in `Assignments`: the terminology split disappears under the concept.

## Limits

- The model is unreviewed; its taxonomy (platform types as kinds of
  Assignment) is absent, so those types gather nothing.
- Rules keep their domain: the move itself, and its id churn with the
  cross-domain matching of `cross-domain-ids-2026-11-04.md`, is not
  measured.
