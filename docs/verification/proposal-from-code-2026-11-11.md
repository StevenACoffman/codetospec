# The proposed concepts, from more of the code, 2026-11-11

The proposal codetospec writes (`generated/concepts.modelith.yaml`) was
checked against the code of assignments. It had 28 entities, every one a
placeholder, with plural names, `1:n` for every owned key, no subtypes and
no enums. Comparing it with the code showed eight gaps; this change closes
seven and asks about the eighth.

1. **Unreviewed models.** A model given to `--domain-model` that still
   holds definitions codetospec wrote (a placeholder, or a draft) is
   flagged: a WARN log, and a line at the top of the README's "Concepts"
   section. That line also says that relationships drawn from foreign
   keys converge with those same keys by construction. The run does not
   refuse: these measurements use the proposal on purpose.
2. **Schema replay** (`extractors/go`). The reader took CREATE TABLE
   only, the first file declaring a table winning, and read `.down.sql`
   files too. On a migrations directory that misses every later change.
   It now applies the files in order as one schema: ADD/DROP/RENAME
   COLUMN, ALTER COLUMN TYPE and SET/DROP NOT NULL, ADD PRIMARY KEY/FOREIGN
   KEY, RENAME TO, DROP TABLE. Rollbacks (`.down.sql`, goose and dbmate
   down sections) are skipped. The reader stays regexp-based: a Postgres
   parser would need cgo, read one dialect, and drop comments.
3. **Primary keys** (fact attribute `primary_key`): an owned key whose
   columns are the child's whole primary key is `1:0..1`.
4. **Descriptions from the code**: SQL comments (above a CREATE TABLE for
   the table, trailing or above a column definition for the column),
   Datastore field comments, GraphQL docstrings. They become attribute
   descriptions and draft definitions. A draft opens with "Draft, from the
   code's documentation:", counts as unreviewed, and is kept as written.
5. **Interfaces**: the webapp extractor emits the GraphQL interfaces an
   owned entity type implements (`api_type`, `interface: true`) and each
   type's `implements`. A type implementing exactly one interface the
   proposal defines gets `subtypeOf`; one implementing several gets none,
   since modelith takes one parent and choosing would be a guess.
6. **Enums**: an attribute whose code type names an `enum` fact (a bare
   name in the table's directory, or `pkg.Type`) takes that enum's type,
   written under `enums:`.
7. **Names**: table entities are singular (`assignments` →
   `Assignment`), the inverse of the plurals matching accepts, so the next
   run matches them back. API types and enums keep their authors' names.
   A Datastore kind stored by a Go type named otherwise names both in its
   placeholder (`stored_by`).
8. **Links nothing declares**: a column outside its table's keys, named
   as the single column of a foreign key another table declares, becomes
   a `column_link` question ("T.c → U"), never a relationship.

## Revisions and commands

- codetospec `79a4ab8` plus this change; webapp `~/khan/webapp` at
  `c55b04b`.
- The scratch copy of `~/spec-graphs/assignments` from
  `concept-alignment-2026-11-11.md`, copied again. Its facts were
  re-extracted with the new extractors (`webapp-extract -root <svc> -lang
  en -exclude-dirs testutil`, `go-extract -root . -skip-dirs testutil`).
  The run used the flags of that record, the LLM on a closed port. The
  first run went without `--domain-model` and wrote the new proposal. The
  next two passed it as `--domain-model`, under the same file name as
  before so the scope stays `assignments`.

## Results

Every run came from the caches: 0 tokens, 0 of 451 chunks failed. Two
runs with the proposal gave byte-identical README, questions and graph.

The facts:

| | before | after |
|---|---|---|
| webapp facts | 2,741 | 2,743 (the two interfaces) |
| `assignments` columns | 15, one of them dropped in 017 | 26: the twelve added later included, the dropped one gone |
| tables with a primary key | 0 of 11 | 11 of 11 |

The proposal:

| | before | after |
|---|---|---|
| entities | 28 | 30 (`BaseAssignment`, `AssignmentStudentData`) |
| placeholders / drafts | 28 / 0 | 14 / 16 |
| attributes, described | 78, 0 | 98, 64 |
| owned relationships `1:0..1` | 0 of 10 | 5 of 10 |
| `subtypeOf` | 0 | 12 |
| enums (attributes typed) | 0 | 3 (3), the change events' types |

The `1:0..1` relationships are the four config tables and
`activity_assignment_writing_coach_student_data`, whose primary key is its
key to `assignment_students`. The 14 placeholders are the four Datastore
kinds (no doc comment) and the ten SQL tables with no comment above their
CREATE TABLE; every API type has a docstring. The placeholder of `SubjectMasteryAssignment` names its Go
type, `CourseMasteryAssignment`.

The run with the new proposal as `--domain-model`:

- The README flags it: 30 of 30 entities unreviewed.
- The placement is unchanged, rule for rule; only the names moved to
  singular (`Assignment` 618 rules, `AssignmentStudent` 56).
- Pairs: 10 converge (the same circular foreign keys), 16 diverge (the
  same pairs), 0 absent, **12 unobservable**: the twelve `subtypeOf`
  links, between API types that have neither a table nor placed rules,
  so nothing could show them.
- Conflicts: the same 11, in the same 4 questions.
- Recovery: adjusted Rand index 0.00 over 162 files (10 concepts, 5
  communities), as before.
- Questions: 144 → 147, the three `column_link` questions, one per
  change-event kind's `assignment_id → assignments`.

## Limits

- No person has reviewed the proposal: every alignment figure above is
  still about codetospec's own reading (TODO's pilot with an expert).
- The three `column_link` questions rest on a column name. A course
  mastery assignment is a Datastore entity, not a row of `assignments`,
  so at least the course change event's link is likely wrong. That is why
  each is a question.
- `assignments.assignment_type` stays a string: SQL columns carry no link
  to the Go enum `models.AssignmentType` (whose six values the enum facts
  do hold, its unit value included).
- API types touch no rule (rules touch tables), so a relationship between
  API types is unobservable to the alignment.
- A draft is the code's documentation as written, stale comments
  included (the `assignment_type` column's comment names values the code
  no longer uses).
- The SQL reader does not follow dollar-quoted bodies: a function body
  holding `;` splits there.
