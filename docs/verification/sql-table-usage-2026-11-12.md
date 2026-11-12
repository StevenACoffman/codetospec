# Shared SQL tables a module queries, 2026-11-12

districts-jobs modules run SQL through the shared pkg's sqlc packages
(`pkg/generated/districtsql`, `assessmentssql`, `snapsql`): constant
queries, called as methods of `*Queries`. None of that reached a module's
graph. A first estimate from the `reach` facts was inflated: 42, 34 and
34 tables for admin-reports, khanx and roster. Reaching the `Queries`
type reaches all 1,093 of its methods.

## The change (`extractors/go/sqltables.go`)

- **What the module calls.** Starting from every function of the module,
  calls are followed through the module and the shared code
  (`calledFuncs`):
  - functions, and methods on a concrete type;
  - an interface method call counts as the methods of that name on the
    named types of the interface's package that implement it (sqlc's
    `Queries` for its `Querier`), nothing wider.

  Function values are not followed, so this is a lower bound.
- **What those functions query.** Each called function's SQL comes from
  its string literals and the package-level string constants it uses
  (`queryNames`), which is where sqlc keeps its queries.
  - The names after FROM, JOIN, INTO and UPDATE count only when the
    shared root's own schema declares them (its SQL replay, the existing
    `sql.go` reader). So CTE aliases and other false matches drop out.
  - A bare name matches a schema-qualified table only when exactly one
    table has that last part.
- **Generated code is read on purpose.** sqlc's files are generated, and
  these facts describe no behaviour, so the generated-file rule for
  rules doesn't apply.
- **The tables used** become table facts cited under the root
  (`table.@<root>/<name>`), with their foreign keys.

## Revisions and commands

- codetospec branch `datastore-kinds` (with #27 and the Datastore change).
- districts-jobs `dde732546`, Go 1.27.4.
- `scandistricts.sh all --quality --crosscheck --acceptance
  --write-domain-model`: 0 tokens in all 22 runs, 0 failures.

## Results

| module               | shared SQL tables | all tables | concept map edges | sqlc references in its code |
| -------------------- | ----------------- | ---------- | ----------------- | --------------------------- |
| admin-reports        | 21                | 20 → 41    | 2 → 12            | 46                          |
| roster               | 10                | 41 → 51    | 21 → 21           | 38                          |
| khanx                | 5                 | 14 → 19    | 2 → 2             | 11                          |
| the other 18 modules | 0                 | unchanged  | unchanged         | 0                           |

- **Rules are unchanged in every module.**
- **The three modules are the ones importing sqlc's packages.** The
  other 18 import none, so their zero is real.
- **The tables are the ones a person would expect.** For example:
  - admin-reports: the `blueprints.*` assessment tables, `courses`,
    `district_class_course`, `agg_district_student_skills_with_time`;
  - roster: `classroom_district_info`, `user_district_info`,
    `window_roster`, `khanmigo_roster`;
  - khanx: `current_district_year`, `district_class_course`,
    `meta_to_districts`, `window_roster`, `khanmigo_roster`.
- **admin-reports' map gains 10 edges,** the foreign keys between the
  tables it queries.

## Limits

- Calls through function values, and interface calls to implementations
  outside the interface's own package, are not followed.
- A query built at run time (concatenated, or read from a file) is not
  read. sqlc's constants are.
- The regex reads names after FROM, JOIN, INTO and UPDATE. The schema
  filter makes a false match harmless, but a table named only in another
  position is missed.
- The schema is the shared root's SQL replay. A table created only by a
  database outside the repository is unknown.
