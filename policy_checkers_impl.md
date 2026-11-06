# Implementation plan: checker recognition and pin vocabulary

Temporary (2026-11-06). It implements the recommendations that followed
the per-checker review of webapp's services. That review is kept outside
this repository, because it describes webapp's access control in detail.
The public numbers are in
`docs/verification/service-checkers-2026-11-06.md`. Delete this plan when
done; measurements go to a new `docs/verification/` file, and they name
counts and code shapes, never findings about webapp's access control.

Baseline: branch `checker-recommendations` from `policy-export`
(`148a972`, which has the `cedarpolicy.ItemType` fix), `make check`
green.

## Working rules

As `policy_gaps_impl.md`:

- write the comment (Requires/Ensures) before the body, and the test
  first;
- run `go test` and `golangci-lint run --fix ./...` in each module
  touched, never relaxing `.golangci.yml`;
- after each step, pause and review it against
  `~/Documents/agent-orange/go-advice/summary_rules.md`;
- take real-data measurements on scratch copies only;
- commit nothing unless asked;
- put no repository-specific code in the core: webapp knowledge stays
  in `extractors/webapp`.

## Measurement (M0, before any change)

What the review found, kept in scratch (a private list):

- 64 checkers the extractor never records;
- 225 recorded checks without a meaning;
- the operations exported on the six scratch graphs.

`M0` is a scratch script. It runs `bin/webapp-extract` on the 32 GraphQL
services and counts:

- (a) unknown checks;
- (b) how many of the 64 names appear in `access_check` facts or
  `check_args`;
- (c) route (operation) facts per service;
- (d) operations matching no schema field.

Each extractor step below states which count it should move. A step that
moves nothing on real code is reverted, not kept.

## Extractor steps (`extractors/webapp`)

### E1. Directive spellings (A1)

`hasDirective` matches the prefix `//ka:permission-check` only. It
should accept `// ka:permission-check` (a space) and the plural
`ka:permissions-check`. The fix normalises the comment text before
matching: strip `//`, trim spaces, accept both forms.

- **Expected:** coaches' `actorCanManageDistrictClassroom`, programs'
  `canEditDevProps`, and coaches' `acl_alias` checker get recorded.

### E2. The decision result of a multi-value checker (A1)

`scanner.bind` binds only the first result of a call, and its last
result when that is the error. Yet `actorKaid, canSetUp, isActing, err
:= permissions.ActorCanSetUpAndEditAdminUDI(…)` decides on its *second*
result, and `targetKaid, isSelf := _setSettingsPermissions(…)` does the
same.

- **Change:** `checker` gains `decision`, the index of the first `bool`
  result of a marked function's declared results, or -1 when it has
  none. `bind` binds that local as the call's access value.
- **Error inside a payload.** A branch that returns, after a denying
  checker's error, a composite literal whose `Error` field is set
  (`return &graphql.VideoMutation{Error: …(err)}, nil`) is a denial: the
  outcome is "payload error". Today `outcomeOf` says "unknown" there. It
  is read only when the error comes from a check whose error denies.
- **Expected:** districts' `ActorCanSetUpAndEditAdminUDI` and
  `ActorCanClearAdminInfo`, users' `_setSettingsPermissions` and its
  marked siblings, and content-editing's `checkEditContentKindPermission`
  family get recorded.

### E3. Checkers wrapped in a variable (A1)

`var X = cache.Cache(f, …)` hides `f`, a marked function (assignments'
`ActorCanViewTeacherClassroomOrCanManageDistrictClassroom` wraps
`_actorCanView…`).

- **Change:** a package variable initialised with a call whose first
  argument is a known checker function (`f` or `pkg.f`), or with the
  function value itself, gets that checker. This is structural: it
  matches any wrapper, not just `cache.Cache`.
- **Expected:** the 18 guards of
  `ActorCanViewTeacherClassroomOrCanManageDistrictClassroom`, and the
  condition-only checks of A1.

### E4. Helper predicates with locals (A1)

`helperPredicates` accepts only a body that is a single `return`.
ai-guide's `actorHasSupportPermissionsForUser` is `scope :=
acl.UserScope(kaid); return acl.ActorHasPermission(ctx, cap, scope)`.

- **Change:** accept bodies made only of `:=` and `var` statements
  followed by one `return`, whose expression (or a local it reads) calls
  a known check. A body with control flow is still not a helper: its
  decision isn't one expression.
- **Expected:** ai-guide's 14 guards, and users' unmarked helpers.

### E5. Condition variables that name no recorded call (A1)

This step is measured after E2-E4. Any condition variable that still
stands for a call with no `check_args` entry gets an entry: its name,
with no meaning. If E2-E4 leave none, the step is dropped.

### E6. Resolver packages reached by embedding (A2, A5)

`resolverScan` reads only `resolvers/*.go`. assessments' root resolvers
embed `*reporting.QueryResolver`, and discussions' embed
`discussions/resolvers.MutationResolver`, so their methods are never
scanned.

- **Change:** follow the embedded fields of the root resolver types to
  the packages they name (through the file's imports, resolved within
  the service), and scan those directories' files too.
  `resolverTypeIndex.typeOf` already names `QueryResolver` → `Query` and
  `DistrictResolver` → `District`.
- **Expected:** discussions' operations go from 6 of 35 schema fields to
  35. assessments' reporting resolvers, including the call sites of
  `AdministeredSchoolsInDistrict` (A5), get recorded.

### E7. Only schema fields are operations (A3)

gap-finder's unexported `canAssignGapFinder` on `*mutationResolver`
becomes `Mutation.canAssignGapFinder`. The fix finds why a private
method's rules carry an operation of their own (`rs.private`, or a
case-insensitive schema lookup). A private method's rules take only its
callers' agreed operation, else none.

- **Expected:** count (d) drops to 0 for private methods.

### E8. Item or call, by what the guard does (A4)

A filter checker's empty result that leads to an authorization error
(`if len(ids) == 0 { return errors.Unauthorized(…) }`) denies the call;
it doesn't drop an item. `ruleVisibility` should only cover a guard that
filters the list it returns.

- **Expected:** assessments' `AdministeredSchoolsInDistrict` and
  assign-content's `QuestionsForClassroom` are scoped `call`.

### E9. Import aliases (A6)

A call on an aliased import (`profilePackage.CanActorAccessStudentProfile`)
is named after the alias. The fix names checks by the imported package's
own name (the last element of its import path), from the file's imports.

- **Expected:** rewards' check appears under one name, with arguments.

### E10. No tautological conditions (A7)

districts' `unmarkDistrictClass` records `any[not X.fails, X.fails]`.
The fix finds the shape (probably an error checked twice, before and
after a branch) with a test from that code shape, and repairs the
condition, or drops the guard when it can't be read.

### E11. Local any-combinators (A8)

users' `_anyPermission(a, b, c)` returns a function that holds when any
of `a`, `b`, `c` holds on its arguments.

- **Change:** a local function whose only parameter is a variadic list
  of functions, and whose body returns a closure that ranges over them
  and returns true on the first that holds, is an any-combinator. A call
  `comb(a, b)(args)`, or `x := comb(a, b)` followed by `x(args)`, reads
  as `a(args) || b(args)`. That condition goes through the existing
  machinery, so each part is recorded with its arguments.
- **Expected:** arguments at all 22 sites.

### Not built (extractor)

- **A9, shared secrets as principals.** Only the two gateways use them,
  and they have no spec graph to export. That would mean a new core
  principal kind for no exportable operation. Recorded as a known limit.
- **A10, one identity for shared copies.** Three of the four copies
  share a name, so P7's merged checkers files cover them by name. A
  body-normalising identity would add a hash scheme for one name.

## Core steps (`internal/policy`, exporters, CLI)

### P1. `subject_arg` on a compound checker (B11)

A top-level `subject_arg` passes to every `self`, `coach` and user-scope
leaf that has none. The error names the leaves still missing one.

### P2. `self` and `coach` in an item filter (B3)

In an item filter, `self` and `coach` are about the item's own user: its
`subject` attribute, which the item resource already carries
(`targetAttrs`). ParseCheckers stops rejecting them there.

- **Expected:** districts' `udi_of_caller` / `coach_of_udi_user` and
  ai-guide's `coach_of` relations can go.

### P3. A capability from a call argument, inside a part (B1)

`capability_arg: N` takes a part's capability from argument N of the
call. It is resolved like the leaf's own capability: argument text →
capability facts.

- **Expected:** users' `canSeeBioFields` and edu-organizations'
  `actorHasPermissionForCountry` can be pinned.

### P4. A relation about an argument's user (B4)

`relation` with `subject_arg` means the caller holds the relation to
that user. The user is bound like a `self` subject, the subject joins
the operation's single subject, and the relation renders on a separate
attribute: Cerbos `R.attr.subjectRelations`, the Cedar policy language
`resource.subjectRelations`.

### P5. Attributes and negation (B5, B6 "holds any capability")

- `{means: attribute, of: caller|resource|context, name: <snake_case>}`
  reads a boolean attribute that the PIP serves, as one set of names per
  holder:
  - Cerbos: `P.attr.attributes`, `R.attr.attributes`, and for the
    context `R.attr.requestAttributes` (beside `R.attr.isTask`, where
    request facts already live);
  - the Cedar policy language: `principal.attributes`,
    `resource.attributes`, `context.attributes`.
- `not: <checker>` negates any checker.
- "Holds any capability" is the caller attribute `has_any_permission`;
  it needs no special leaf.

### P6. Scopes: locale and explicit grants (B2, B6)

- `scope: locale` matches a grant for the request's locale, by analogy
  with `country`: Cerbos `P.attr.localeGrants`, the Cedar policy language
  `principal.localeGrants.contains({capability, locale:
  context.locale})`.
- `scope: explicit` matches any grant except the admin role's implicit
  ones. The loader puts such grants in a `CapExplicit` group, by analogy
  with `CapUserAll`.
- Content-kind scopes are not built: their mixed scope keys need the
  grant model, and there are 4 checkers.

### P7. Unpinnable entries; several checkers files (C2, C4)

- An entry `{unpinnable: <reason>, note: …}` states why a checker can't
  be pinned. The export reports that reason instead of "check outside
  the vocabulary".
- `--checkers` may repeat. Files are merged, and a name defined twice is
  an error naming both files. That is how shared (`pkg/`) pins and
  per-service pins combine.

### P8. Stale pins (C1)

- An entry may carry `digest`, the digest of the checker's definition
  span (the `access_check` fact's source) when the pin was written. The
  digest is computed by `drift`'s span hashing, under the same roots.
- `export` and `policies` warn when a pin's digest no longer matches,
  and the summary counts stale pins.
- `policies --format json` prints each checker's current digest, so a
  person can record it.
- Reading files stays in the CLI: it computes the digests (through
  `drift.Digest` and `extract.Roots`) and passes them, as a map, to a
  pure `policy.StalePins(checkers, digests)`.
- A pin without a digest is never stale. A stale pin is still applied:
  failing the export would only push people to delete digests.

### Not built (core)

- **B7, pins that depend on an argument.** Five districts checkers are
  pinned at their strictest reading (approx), which never over-permits.
  Literal-argument keys would add a matching dimension for that.
- **B8, two subjects.** This changes every leaf's binding, the case
  oracle and both renderers. It needs its own plan, measured on the 13
  checkers.
- **B9, cross-service join.** It depends on Part 2 step 8 (the
  multi-graph inventory join), which is not written.
- **B10, rate limits as obligations.** Whether to allow what the code
  throttles is a policy decision for the people running the export, not
  a refactor. It stays a skip until it's asked for.
- **C3, the `pin` command.** The LLM proposal loop needs its own design
  and evaluation, like the crosscheck's.
- **C5, bugs as questions.** It needs the pin author's notes as data,
  which P7's `note` starts.

## Go-advice review of this plan

- **§15, design it twice** (for the four steps with a real choice):
  - **E2:** binding the decision result by its declared type (AST: the
    first `bool` result of the marked function), or loading `go/types`
    for the service. AST chosen: the webapp extractor reads ASTs only,
    and loading packages would make it depend on a buildable checkout
    for one fact.
  - **E11:** rewriting a combinator call into an `||` of calls, or a new
    guard kind for combinators. Rewrite chosen: every later stage
    (condition, `check_args`, meanings, subject binding) works unchanged
    (§4, pull complexity down).
  - **P5:** an `attribute` leaf, or one more relation per fact. Attribute
    chosen: a relation joins caller and resource. Unary facts written as
    relations were the workaround the review had to invent (`not_deleted`
    as a relation), which is information leakage between the two ideas.
  - **P8:** a digest inside each entry, or a lock file beside the
    checkers file. Inline chosen, as `targets.yaml` does: one file a
    person owns, and no second file to keep in step.
- **§4:**
  - The common case needs the fewest fields: P1 accepts `subject_arg`
    where everyone first wrote it.
  - No special case in a general mechanism: E3 matches any wrapper whose
    first argument is a checker, not `cache.Cache` by name.
  - E4 replaces the single-return rule with the general one, leaving no
    two paths.
- **§5:**
  - P8's file reads stay in the CLI, with `StalePins` pure.
  - P7's merge is a pure function over parsed files; the CLI reads them.
- **§3:** P7's duplicate names and P8's unreadable spans are reported,
  never silently ignored. An unreadable span is a warning naming the
  checker, because the export itself doesn't depend on it.
- **§16, coupling dimensions:** P4-P6 add attributes to the loader
  contract (the principal's `attributes` and `localeGrants`, the
  resource's `attributes` and `subjectRelations`, the context's
  `attributes`). They are named in each exporter's package doc, the one
  authoritative place, and listed in the verification file.
- **§15, YAGNI and delete dead code:**
  - E5 is conditional on measurement.
  - Every extractor step must move an M0 count, or it is reverted.
  - The "not built" items each state the carrying cost that keeps them
    out.
- **§9:** each step is test-first, through the real path where AGENTS
  asks (extractor goldens, then the `graph` contract test, then
  `TestExportCheckers`).

## Verification and docs (V)

- `make check` and `make corpus`.
- The extractor goldens (`UPDATE_GOLDEN=1`, reviewed).
- M0 rerun:
  - counts (a)-(d) per extractor step;
  - the six scratch graphs re-extracted (warm map cache: the extractor's
    mechanical rules change the map prompt input, so the map spends
    some tokens), then exported with the review's checkers files,
    updated for P1-P7;
  - `cerbos compile` and the Cedar policy language validation on each.
- Docs:
  - a new `docs/verification/` file (counts and code shapes only);
  - AGENTS.md: policy, webapp extractor;
  - README;
  - `extractors/webapp/README.md`.

## Progress (2026-11-06)

M0 counts on the 32 GraphQL services: (a) unknown checks, (b) the 64
checkers the review found unrecorded, now recorded, (c) route facts,
(d) operations matching no schema field.

| step | (a) | (b) | (c) | (d) |
|---|---:|---:|---:|---:|
| M0 | 225 | 0 | 1604 | 3 |
| E1 spellings | 227 | 2 | 1604 | 3 |
| E2 decision / nil result | 235 | 10 | 1604 | 3 |
| E3 wrapped checkers | 236 | 11 | 1604 | 3 |
| E4 helpers with locals | 237 | 12 | 1604 | 3 |
| E5 unknown calls in denials | 289 | 22 | 1604 | 3 |
| E6 embedded resolver packages | 301 | 30 | 1646 | 3 |
| E7 schema fields only | 301 | 30 | 1646 | 0 |
| E8 empty filter denies the call | 301 | 30 | 1646 | 0 |
| E10 decision and failure of one call | 301 | 30 | 1646 | 0 |

- **E2:** the nil-result case covers checkers whose results carry no
  `bool`. The payload-error branch needed no change, because its outcome
  only affects wording. Review: one `failure` enum (error or nil) instead
  of two parallel mark sets; `failedCall` holds the flip; the dead
  `binding.access` field was removed.
- **E5:** check calls carry `form: call`, so `access_check` facts no
  longer guess a form from the name.
- **E6:** discussions now has all 35 of its Query and Mutation fields
  (it had 6). A non-deterministic choice between nested directories that
  end alike was caught by running M0 twice, and fixed: the longest
  directory wins.
- **E8:** item rules went from 107 to 84. assign-content's
  `QuestionsForClassroom` stays `item`: its denial is an error passed
  through from another service, which nothing in its code marks as a
  denial (a known limit).
- **E10:** 17 tautologies, now 0. The root cause: one call node bound
  twice (decision and error) was named after its failure in both places.
  The fixture's goldens had two of them.
- **Not built:**
  - **E9.** Rewriting aliases at parse time collides with E6, because
    discussions imports two packages named `resolvers` under aliases, and
    a scoped alias map would reach every lookup for one case. E5 already
    records rewards' aliased call once, with its arguments.
  - **E11.** 20 of `_anyPermission`'s 22 sites pass the combinator to a
    helper (`_getLoginWithPermission`), which needs higher-order
    inlining; the 2 direct sites don't pay for a recognizer. The
    variable mismatch those 2 sites exposed (`_anyPermission(…)` in the
    condition, `_anyPermission` in `check_args`) is fixed: a call of a
    call's result is named after the inner check.
- Of the 34 checkers still unrecorded, most are data-access or
  cross-service decisions (`canEditDevProps` passed into
  `program.ReviseProgram`), outside what a guard scan can see.
- **Core steps P1-P8: done.** Each was test-first, in
  `internal/policy/vocabulary_test.go`, `TestExportCheckers` and
  `TestExportStalePins`, plus the renderers' agreement tests extended
  with every new kind (`cerbos compile`: 98/98 OK; the Cedar policy
  language: 97 cases).
  - P1: `withSubjectArg` copies, never mutates; the error names the part
    ("part 2: …").
  - P2: `SubjectItem`.
  - P3: `capability_arg`.
  - P4: a relation with `subject_arg` renders `subjectRelations` and
    binds the operation's subject.
  - P5: the `attribute` leaf, by holder, and `not` (an inner part named
    `<check>#1`, so the oracle never confuses the checker's variable with
    its negated part).
  - P6: the `locale` and `explicit` scopes (the Cedar policy language
    `LocaleGrant` and `CapExplicit`).
  - P7: `unpinnable`/`note`, and `MergeCheckers` behind the repeatable
    `--checkers`.
  - P8: `Checker.Digest`, `policy.StalePins` (pure), digests computed in
    the CLI (`checkDigests`, `drift.Digest`) only with `--src`.
  - Review fixes: `Checker.aboutUser` was dead after P2 (only
    self-recursive, so `unused` missed it) and is deleted; `check` is
    split into shape, parts and leaf (gocognit); the Cedar test's
    `world.addName` holds the list choice.
- `make check` and `make corpus` green.
- **V, real exports: the first run found an E5 false positive.** A
  denying guard that turns a loader's error into an authorization error
  (`user, err := user_data.GetByKAID(…); if err != nil { return
  …Unauthorized }`) named the loader as a check. `checksBelow` then
  correctly withheld every operation reaching it below its guards: users
  went from 112 to 88 operations, and every suite still passed (nothing
  over-permitted, it just withheld more).
  - Fix: an unknown call is named only where its result decides (a bare
    call), or where its error is its only result (`err := Validate(…)`,
    a new `failsWithSoleError`). A value-plus-error call's failure is
    data.
  - M0 (a) goes from 301 to 298; (b) stays at 30.
- **V, after the fix.** The exports (before → after, same checkers):
  admin 37 → 37, districts 298 → 297, assignments 115 → 114, users
  112 → 112, coaches 163 → 157, ai-guide 151 → 159. Every
  `cerbos compile` is OK and every Cedar policy language export
  validates. Users with one entry in the new vocabulary
  (`canSeeBioFields`: `capability_arg`, `subject_arg` on the compound)
  exports 118. The operations withheld reach a newly recognized check
  below their guards (`checksBelow`). The verification doc has the
  table: `docs/verification/checker-recognition-2026-11-06.md`.
