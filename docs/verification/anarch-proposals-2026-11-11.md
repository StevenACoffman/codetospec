# Architecture rule proposals: cross-service calls and stored state, 2026-11-11

TODO's "Propose structural rules to anarch-lint" asks codetospec to emit
candidate `fitness` rules for a person to accept into a target's
`anarch.json`. Rule text gave none (2026-11-02), nor did the per-layer
`nature_mismatch` rates or the enum transitions (2026-11-04). That second
measurement re-scoped the item to the writers of state fields. This
measures that source and one other. Both rest on something anarch-lint
does not know itself:

1. **Cross-service calls:** which service owns each GraphQL operation a
   service calls.
2. **Stored state:** which Go structs are stored as Datastore kinds, and so
   which enum-typed fields are state.

The other candidates were rejected without a measurement:

- **Imports and calls in general:** anarch-lint loads the code itself, and
  its `draft` command already freezes today's imports.
- **Capability checks:** "only today's callers may check a capability"
  would forbid what every resolver should be able to do.
- **Concepts:** on assignments the busiest directory serves 11 concepts
  (`concept-alignment-2026-11-11.md`), and no person has confirmed a
  model yet.

## Revisions and commands

- codetospec at `c51f9d7`; webapp `~/khan/webapp` at `c55b04b`, a clean
  tree; anarch-lint at `e14bcf2`.
- Services: admin, ai-guide, assignments, coaches, districts, users. Each
  was extracted with `webapp-extract -root <svc> -lang en -exclude-dirs
  testutil` and `go-extract -root . -skip-dirs testutil`, into a scratch
  directory.
- Every candidate was checked by anarch-lint on webapp itself:
  `anarch-lint -C ~/khan/webapp --model <model> fitness ./services/<svc>/...`
  (33 s for assignments).
- For source 1, the webapp extractor carried an attribute naming each
  `dependency` fact's operation function. It was reverted after the
  measurement.
- For source 2, a throwaway program listed the enum-typed fields:
  - an enum is a named type with at least two constants of it in its
    package;
  - a field counts when its type, or what it points to, is an enum;
  - fields in generated packages and `testutil` were dropped.

  A `fields` rule with `writes: true`, `only: true` and no `from` then made
  anarch-lint report every write, naming its writer. Writes from `testutil`
  were dropped.

## Source 1: cross-service calls

Every webapp service sends its calls to other services from one
`cross_service` package, plus a few one-off `cmd/` tools. The candidate
for each service S and each service T it calls is a `calls` rule with
`only: true`:

- `from`: the packages that call T's operations today;
- `callees`: the genqlient operation functions whose root fields T owns.
  They must be listed one by one, because `genqlient.*` would also match
  the getters of the generated response types, which resolvers call.

Every `dependency` fact resolved to a caller package and a callee function.

| service     | rules | callees matched | violations | callers besides `cross_service`                   |
| ----------- | ----- | --------------- | ---------- | ------------------------------------------------- |
| admin       | 2     | 9               | 0          | `cmd/make_admin`                                  |
| ai-guide    | 15    | 118             | 0          | `cmd/export-enrollments`, `cmd/export-waitlist`   |
| assignments | 8     | 48              | 0          | none                                              |
| coaches     | 10    | 67              | 0          | none                                              |
| districts   | 13    | 99              | 0          | `cmd/kadss_test_district`, `cmd/seedpartnerships` |
| users       | 9     | 46              | 0          | `cmd/create_dev_users`                            |

- 57 rules, every one holding today. ai-guide and users each report "4
  unseen": dynamic calls the call graph resolves to nothing.
- 49 rules carry `security`: at least one of their calls uses
  `AsServiceAdmin()`.
- Control: dropping `cmd/make_admin` from admin's users rule fails it, on
  `services/admin/cmd/make_admin/main.go:56`.

But codetospec should not propose these rules:

- **The lists go stale weekly.** A rule names functions, so every new
  operation calling another service needs a new proposal and a new
  acceptance. Between 2026-08-11 and 2026-11-11,
  `generated/genqlient/queries.go` changed in 28 commits for ai-guide,
  19 for districts, 14 for assignments, 13 for coaches, 2 for users and
  none for admin. A rule that misses a new operation also misses the
  very call the convention exists to stop.
- **Deferred calls escape both the extractor and the rule.**
  `tasks.GraphQLTask` takes an operation's `_Operation` text and runs it
  later. In assignments, 3 of the 11 operations `tasks` queues go to
  other services (2 to notifications, 1 to ai-guide). The other five
  services hold 2 to 10 such references each, in `tasks` or
  `cross_service`; their owners were not sorted out here.
- **One rule per service could state the convention.** The ownership is
  standard Apollo Federation 2 markup (`@join__field(graph: …)` in
  `gengraphql/composed_schema.graphql`), and anarch-lint's `graphql` rule
  already parses genqlient operation documents. A rule reading the
  supergraph could say "operations whose root fields another subgraph
  owns are used only from these packages". It would cover new operations
  without an edit, and could count a reference to an `_Operation`
  constant as well as a call. That belongs in anarch-lint, is not
  webapp-specific, and needs no codetospec output.

## Source 2: writers of stored state

| service     | Datastore kinds | enum-typed fields | written | by 1 package | by 2 | by 3+ |
| ----------- | --------------- | ----------------- | ------- | ------------ | ---- | ----- |
| admin       | 3               | 1                 | 1       | 1            | 0    | 0     |
| ai-guide    | 33              | 23                | 16      | 7            | 6    | 3     |
| assignments | 4               | 3                 | 3       | 3            | 0    | 0     |
| coaches     | 14              | 3                 | 3       | 3            | 0    | 0     |
| districts   | 43              | 26                | 15      | 14           | 1    | 0     |
| users       | 34              | 8                 | 8       | 6            | 2    | 0     |
| **total**   | 131             | 64                | 46      | 34           | 9    | 3     |

- The candidate for each written field is a `fields` rule with
  `writes: true` and `only: true`, `from` listing today's writers. That's
  46 rules, about 8 per service, each holding today by construction.
- Most single writers are the domain package that owns the state:
  - districts: `emails` for `KADEmailRun.Status` and `KADEmailDispatch.Status`;
    `rostering` for `DistrictOnboardingEntity.CycleType`;
  - users: `user_role` for `RoleChangeRecord.Role`;
  - assignments: each change event's `Type` written by its own package.
- Where a field has several writers, the domain package usually shares it
  with `models`, `resolvers` or a `cmd/` tool, e.g. ai-guide's
  `AIGuideAccessPlan.LifecycleType` by `access_plan_manager`,
  `cmd/access-plans` and `models`.
- Without the Datastore filter, the same scan finds 378 enum-typed fields
  and 284 written. A sample of 24 single-writer fields from districts and
  assignments was mostly request, parameter and response structs
  (`ModifyDistrictParams`, `CreateInput`, `ValidationError`). Knowing
  which structs are stored is what makes the proposals worth reading, and
  codetospec knows it (the webapp extractor's `table` facts and
  `stored_by`); anarch-lint does not.
- A rule names fields, which change rarely, unlike operation lists.
  Freezing the writers is the point: a new writer becomes a reviewed
  change.

## Conclusion

- Stored-state writers are worth a proposals file: about 8 per service,
  stable, readable, and resting on codetospec's knowledge of what is
  stored.
- Cross-service calls are not worth proposing from codetospec; they need
  a native anarch-lint rule.

## The export, end to end

Built after the measurement:

- **Go extractor:** `writes` facts.
- **codetospec:** `export --format anarch` (`internal/anarch`).
- **anarch-lint:** the proposals format, `schema --proposals`,
  `proposals report` and `proposals accept`.

Run on the same six services:

- The Go facts were re-extracted with the new extractor.
- Each graph of `~/spec-graphs` was copied with those facts.
- Each was exported, then `anarch-lint -C ~/khan/webapp proposals
  report <file> ./services/<svc>/...` ran with no model.

| service     | state writes (facts) | proposals | passed |
| ----------- | -------------------- | --------- | ------ |
| admin       | 2                    | 1         | 1      |
| ai-guide    | 400                  | 16        | 16     |
| assignments | 90                   | 3         | 3      |
| coaches     | 25                   | 3         | 3      |
| districts   | 250                  | 24        | 24     |
| users       | 21                   | 8         | 8      |

- 55 proposals, every one passing on today's code. Their evidence holds 128
  writes and 231 rules citing the writing code; 11 proposals have no such
  rule.
- A first version took the rules `touching` the stored entity as evidence:
  3,376 entries, about 70 per districts kind, too many to read. Citations
  overlapping a writing function are what say what the write does.
- 55 is more than the measurement's 46 because `testutil` writes count
  here. The run drops only rule and domain facts under an excluded
  directory, and anarch-lint loads `testutil` (it isn't a `_test.go`
  file), so a rule leaving it out would fail.
  - 9 districts proposals name `testutil` as the only writer: fields set by
    fixtures, and otherwise by their own type's methods. A reviewer will
    likely reject them.
  - 14 more list `testutil` or a `cmd/` tool beside the domain package.

## Limits

- Six services, one webapp commit.
- "Holds" is anarch-lint's static analysis: the 4 unseen calls in ai-guide
  and users are not checked, and a write through reflection or the
  Datastore API's property loading is not a field write.
- The enum test (two constants of a named type) differs slightly from the
  Go extractor's `enum` facts.
- Only Datastore kinds: the Go extractor's SQL `table` facts name no Go
  type, so the Postgres-backed state of assignments (the `assignments`
  table) is not covered.
- Whether a person would accept these rules was not measured; that is
  the pilot.
