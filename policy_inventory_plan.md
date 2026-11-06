# Plan: resolver journeys, a policy inventory, and policy exports

Temporary plan (2026-11-06). It is the codetospec side of comparing
extracted access rules with the PIP proposal
(`~/Documents/git/ident-know/webapp/datamodel.md`). The webapp bugs found
on the way are in `~/Documents/git/ident-know/webapp/auth_bugs_found.md`.
Once a step ships, its measurement goes in a new `docs/verification/`
file and this plan shrinks; delete it when all three parts are done.

**Implementation status** (2026-11-06): Parts 1-3 are implemented
(`policy_inventory_impl.md` tracks each step and its deviations), each
measured under `docs/verification/`. Open: registry entries naming
methods; pinning the meaning of service-local checkers and admin's
wrappers (Part 3, R4), the largest gap left in what the exports
translate; the cross-service join of inventories (Part 2, step 8).

Measurements run on warm caches, so the deterministic parts can't vary
between runs. Warm isn't free when a change adds extractor rules (the
map shows each chunk its `MECHANICAL_RULES`, so those chunks re-map) or
changes a rule's nearest tests (the crosscheck key folds them in): both
were measured, a few tens of thousands of tokens on districts.

Across a remap with no prior output to take ids from, compare counts,
never rule lists: totals move 0.4 % (about 2 % within a nature), but
about half the rules have no match in the other run
(`docs/verification/remap-2026-11-05.md`).

Revisions measured: codetospec `ccc0566`, webapp `c55b04b0090`, the
remap outputs of 2026-11-05 under `~/spec-graphs`, cedar-go `7f6b40e`,
and the cerbos checkout at `~/Documents/git/cerbos`. "authz §N" is a
rule of Windley's *Authorization in Action*, distilled in
`~/Documents/git/ident-know/rules/authorization_in_action_book_rules.md`
(its "Cedar" is always the Cedar policy language).

**Two names, two things.** *CEDAR* is Khan's analytics event pipeline:
webapp publishes events such as `UserRoleAdded` to it (each service's
`generated/analytics_events`, the `cmd/cedar_*_changed` pullers). The
*Cedar policy language* is the open-source authorization policy language
and its PDP (cedar-policy.com, `github.com/cedar-policy/cedar-go`). This
plan exports to the Cedar policy language, never to CEDAR, and always
names it in full. Code follows suit: `--format cedar-policy` and
`internal/cedarpolicy`, never a bare `cedar`, so that nobody reads an
export to the CEDAR analytics pipeline into it.

## Findings

What the comparison showed about codetospec's output. Each was checked
against the code; findings 6-12 come from reviewing this plan against
webapp.

1. **"Allow any caller" is a declaration, not a gap.** The webapp
   extractor writes "The system shall allow any caller to call X" only
   for a top-level `acl.OpenAccess()` (`extractors/webapp/authz.go:34`,
   `aclMarker` at `:924-940`, `marker` at `walk.go:104-117`, depth 0
   only). districts has 148 such resolvers. In a sample of 17:
   - 11 have no check at any depth: the identifier is the permission, or
     the parent object checks;
   - 5 check deeper;
   - 1 is scoped to the current user.

   A resolver with no recognized guard gets no rule at all, so the
   extractor cannot say which resolvers have no check.
2. **GraphQL endpoints have no journey.** 12 of districts' 209 endpoints
   carry `journey:`, all of them `/pubsub/...` push handlers from the Go
   extractor.
   - The 197 GraphQL route facts have `handler_path` but no
     `handler_lines`: `routeFact` (`extractors/webapp/analyze.go:582-597`)
     never sets it, though `resolverMethod` holds `start` and `end`
     (`authz.go:20`).
   - `graph.handlerSpans` (`internal/graph/handlers.go:46`) skips a route
     without both, so neither `withJourneys` nor `linkHandlers` sees
     resolvers.
3. **The rules name the checks only in prose.** The counts below are
   totals over the seven webapp outputs (admin, ai-guide, assignments,
   coaches, districts, graphql-gateway, users).
   - A regex over the access rule facts' requirement and condition text
     finds 59 names. About 55 are checks; the rest are scope arguments
     (`acl.AnyScope`, `acl.GlobalScope`), the `acl.TODO` marker, or one
     check spelled two ways.
   - It misses about 114 more helpers that the same rules name in
     backticks (`actorIsTeacherOf` 31, `canGrantAiGuideAccess` 19,
     `isUserTeacherOfClassrooms` 13, `acl_alias.*`, `_anyPermission` …).
   - It also misses checks recorded only as condition variables
     (`request.isServiceAdmin` in 306 rules, `caller.loggedInNonPhantom`
     in 114), and rule forms other than guards: 47 role grants (46 plus
     admin's leaked `testutil` role, `FIX.md` F2), 11 policies, 90
     "check access with X", 58 header contracts.
   - The extractor knows every check structurally (`checks.has`,
     `authz.go:318`: marked `//ka:permission-check` checkers,
     `accessPredicates`, `helperPredicates`, acl calls) and drops that
     knowledge when it renders the sentence.
   - The condition keeps a marked checker as a bare variable named after
     the function. Its arguments are dropped by design: "the notation has
     no call operands" (`extractors/webapp/condition.go:191-194`). So
     admin's `actorHasGlobalPermission(ktx, r.IAMClient,
     capabilities.CanGrantContentRoles)` becomes the variable
     `actorHasGlobalPermission`, with no capability.
4. **What a check reads is not recorded anywhere.** Comparing a check
   with the PIP registry needs the stored fields, capabilities, flags and
   request properties it reads. Today that takes reading the code. For
   districts' `permissions.go` it gave, per function:
   - fields such as `ClassroomDistrictInfo.AllTeacherUDIsIDs`,
     `UserDistrictInfo.StudentOfCDIKeys`, `UserDistrictInfo.PrimarySchoolID`,
     `UserMetaInfo.CanManageAdmins`, `District.OfferingValues`;
   - the `moa-enforcement` flag;
   - capabilities with their scope.

   The LLM rules citing those functions describe the decisions well
   enough to find them, but their conditions use `local.*` variables that
   name the shape, not the field.
5. **The inputs the PIP lacks are of a few structural kinds.** From the
   manual comparison:
   - self (`acl.IsCurrentUser`, the most common check);
   - relations;
   - attributes on an edge (`CoachCoacheePair.
     CoachCanManageUAGCoacheeAccount`, `SpawnedBy`);
   - user attributes (`isChild`, `isFormalTeacher`);
   - resource attributes and entitlements (`District.OfferingValues`,
     access-plan groups);
   - staff capabilities with a scope: 20 of districts' 28 decision
     functions open with a service-admin or `CanActLikeADistrictAdmin`
     bypass;
   - feature flags;
   - request context (service-admin query, task, country).

   The inventory should sort what each check reads into these kinds from
   structure, never from words.
6. **Capabilities are package-level variables, often in generated
   files.**
   - A service names a capability through its own
     `generated/capabilities` (`var CanChangeUserRoles
     permissions.CapabilityName = "CAN_CHANGE_USER_ROLES"`): districts'
     file has 127 `var`s and no `const`.
   - admin defines them as `var`s initialised by
     `RegisterCapabilityDefinition` or `RegisterCapabilityName`
     (`services/admin/permissions/capabilities/`).
   - Feature flags are constants (`const MoaEnforcementFlag
     featureflags.FeatureFlagName = "moa-enforcement"`,
     `feature_flags/moa.go:11`). But the Go extractor's `constant` facts
     hold numeric constants only (`extractors/go/constants.go:15-35`), so
     neither capabilities nor flags have a fact today.
7. **The webapp extractor emits nothing for `pkg/`.**
   - `scanwebapp.sh` runs it only for services (`if [ "$service" != pkg
     ]`, `scanwebapp.sh:165`), and `~/spec-graphs/pkg/.codetospec` holds
     Go facts only.
   - The extractor reads pkg's marked checkers when it analyses a service
     (`-webapp`, `extractors/webapp/main.go:58`), but emits no fact for
     them.
   - So `pkg/khan/acl`'s checks (`ActorHasPermission`, `IsCurrentUser`,
     `IsCoachedByActor`, the scope matchers) have no fact to refer to.
8. **Checks load entities by query, and closures break at package-level
   variables.**
   - A check finds a user's UDIs through `LoadActiveUDIs`
     (`permissions/permissions.go:357`), which calls a dataloader held in
     a package-level variable: `var udiLoaderByKaid =
     loaders.LoaderForQuery(datastore.NewQuery(models.UserDistrictInfoKind),
     "kaid", …)` (`:348-353`).
   - Lookups filter by property-name strings: `FilterField("district…")`
     104 times and `FilterField("kaid…")` 93 times in districts. A
     selector read of a struct field sees neither which kind is loaded
     nor by what key.
   - Existence inputs (the registry's `hasUMI`: "a non-deleted UMI with
     this kaid") are loads, not reads.
   - The Go extractor's call facts take both callers and callees from
     functions, methods and named types only (`moduleDecls`,
     `extractors/go/symbols.go:26-60`, `:123-160`). A call through
     `udiLoaderByKaid(ctx)`, or through admin's `var
     UserScopesForCapability = cache.Cache(_uncachedUserScopesForCapability)`
     (`services/admin/permissions/scopes.go:89-90`), ends the closure.
     The `cache.Cache` pattern is common in webapp.
   - This is the same blind spot as `FIX.md` F3, where tests that name a
     package-level `var` reach nothing.
   - `struct` facts don't record `datastore:` tags
     (`extractors/go/structs.go`), so a filter string can't be mapped back
     to its field.
9. **A scope name means what its matcher does, which differs by
   checker.**
   - `pkg/khan/acl.UserScope(kaid)` accepts a global grant, or an
     `nwea-map` grant when the target user's data is KMAP-district-owned:
     a runtime GraphQL lookup (`pkg/khan/acl/permissions.go:205-220`).
   - districts' own `_checkActorCapabilityScopes`
     (`permissions/permissions.go:298`) accepts only `UserKind == "all"`
     (`auth_bugs_found.md` #8).
   - So a check's scope must be recorded as the matcher used, never as
     one meaning per scope name.
10. **The registry's relations are conditional, and some are read
    through methods.** The datamodel's Relations table maps:
    - `admin_of` to `AdminOfSchoolKeys` *when `!IsGlobalAdmin`*;
    - `AdminOf` to districts or meta-districts *by `AdminOfHeight`*;
    - `teacher_of` to `AllTeacherUDIsIDs` *minus grantees*.

    Checks also read these fields through methods:
    `DirectlyAdministersSchools()`; `GetStudentOfCDIsIDs()` in
    `CanViewCDI` (`permissions/permissions.go:768-770`);
    `IsDistrictOrSchoolAdmin()`, which reads `PrimaryRole`. A registry
    that maps a relation to a flat list of fields can't express that.
11. **Only resolver methods can be enforcement points.** gqlgen fields
    resolved by struct binding have no method, so no rule and no
    `unguarded` fact can name them. Their access is the parent object's.
12. **Guards are top-level by construction.**
    - `resolverMethod` holds "the access guards found at the top level of
      its body" (`authz.go:14-15`), so a guard rejects the whole call.
    - The exceptions are guards inside a range loop (`guard.loop`,
      `perItem`), which reject one item and carry on, and visibility
      filters, which drop results.

## Part 1: resolver journeys (`handler_lines`)

Checked against the code: one attribute is enough. Journeys match a
handler span to the Go extractor's call facts by overlap
(`span.overlaps`, `internal/graph/journeys.go:77`), not by equality.

### Change

1. `extractors/webapp/facts.go`: add `attrHandlerLines = "handler_lines"`
   beside `attrHandlerPath`, documented as the resolver method's span.
2. `extractors/webapp/analyze.go`, `routeFact`: set
   `attrHandlerLines: lines(m.start, m.end)`. `all.go:141` already
   rewrites `handler_path` for the multi-service run; lines need no
   rewrite.
3. Nothing in the core changes: `handlerSpans` already reads both
   attributes, and `withJourneys` and `linkHandlers` start from them.

Design note: the span is the resolver method only. A rule the extractor
finds in a private helper (`operation.via`) is reached through the Go
extractor's call facts, like any other callee, so journeys need no
special case for it. A call through a package-level variable ends the
journey, as it ends every closure (finding 8).

### Effects to expect and check

- **Journeys:** every GraphQL endpoint whose resolver reaches cited code
  within `journeyDepth` (3) gains `journey:` and a body list. The call
  facts exist: districts' `go.facts.json` has 1,848 calls starting in
  `resolvers/` (of 16,689).
- **`exposed_by` edges:** expected to grow (`linkHandlers`), moving
  nature evidence, `nature_mismatch` questions and concept placement.
  *Measured (2026-11-06): they don't move.* Every rule citing resolver
  code already had its edge, through the extractor's `route` attribute or
  the map's endpoint lists, so only the journeys change
  (`docs/verification/resolver-journeys-2026-11-06.md`).
- **Caches:** route facts are outside `promptKey` (the allowed endpoint
  lists are not in it), so a warm rerun maps and reduces nothing.
  Crosscheck and acceptance keys hash rule and cited lines, so they stay
  warm. Confirm by token counts in `state.json` before and after.
- **Corpus:** no baseline should move. The corpus re-measures the output
  graphs for rules, grounded, ungrounded, proven, partial domains,
  violations, drifted and `ears_issues`; it counts neither edges,
  journeys nor questions, and coverage doesn't depend on them. Confirm
  with `make corpus`.
- **Unblocks** the `TODO.md` item "Concept evidence from endpoints"
  (DOMAIN_PLAN.md recommendation 12: rules reached by the journeys of an
  endpoint whose type the model owns), which needs GraphQL journeys.

### Steps

1. **Test first.** In `extractors/webapp/analyze_test.go` (or the golden
   test), a resolver method of known span yields a route fact with
   `handler_lines` equal to it. Regenerate the goldens
   (`UPDATE_GOLDEN=1 go test ./...` in `extractors/webapp`) and review
   the diff: only `handler_lines` lines added.
2. **Core test through the real path.** A fixture service under
   `extractors/webapp/testdata/service` whose resolver calls a helper
   with a cited rule. The built graph's endpoint has that rule in its
   journey, and the helper's rule has `exposed_by`. (AGENTS.md: test
   per-file computations through the real path, not hand-built facts.)
3. `make check` in the root and in `extractors/webapp`
   (`golangci-lint run --fix ./...`, never relaxing `.golangci.yml`).
4. **Measure** before and after on districts and users with a warm rerun
   (`scanwebapp.sh <svc>` with the run's flags, after rebuilding
   `bin/codetospec`):
   - endpoints with a journey, and the distribution of journey lengths;
   - `exposed_by` edges added;
   - `nature_by_evidence` counts by value, and the agreement with
     `scripts/domainsplit.py` that `docs/verification/
     nature-by-evidence-2026-11-04.md` measured (94-95 %): it must not
     drop;
   - `nature_mismatch` questions, concept assignment counts;
   - LLM tokens of the rerun (expected: none for map and reduce).
5. **Record and document.** Write
   `docs/verification/resolver-journeys-<date>.md`. In AGENTS.md, the
   `graph/journeys.go` paragraph says journeys come from handler spans:
   add that webapp resolvers have them. Update the webapp extractor
   README. Re-record the corpus after review.

### Risks

- A resolver span can be long (generated-style resolvers that inline
  work): `exposed_by` then links rules that only share the method. That
  is what the edge means; the measurement shows whether business
  evidence inflates. If it does, the right test is the cited lines
  overlapping the span, not the position, and the fix belongs in
  `position`, not here.
- One endpoint per resolver method: a method serving several fields
  (rare in gqlgen) gets the same span for each.

## Part 2: a policy inventory built from the rules

### Goal

For each service, one table:

- every enforcement point (endpoint or field resolver);
- the checks it makes;
- for each check, what it reads, sorted into structural kinds;
- for each input, whether a given registry (the PIP's) declares it.

Deterministic, no LLM, and complete in a stated sense. Every access check
the extractor recognizes appears, and every resolver method it doesn't is
listed as such. Fields with no resolver method (finding 11) are outside
by definition, and the output says so.

### Design

Functional core, imperative shell, as the rest of codetospec: the
extractors emit facts, a pure core package (`internal/policy`) builds the
inventory from facts and rules, and the CLI writes it.

**Extractor facts (webapp).**

1. **An `access_check` fact per check definition the service has**:
   marked checkers, access-package predicates, helper predicates.
   - ID `access_check.<pkg>.<Func>`.
   - Attrs: `name`, `form` (`marked`, `predicate`, `helper`, `acl`), and
     `filter` and `err_denies` when marked.
   - Source: the function's declaration.
   - The shared checks of `pkg/` (`pkg/khan/acl` and the other marked
     checkers the extractor already reads through `-webapp`) get facts
     too, cited under the `pkg` root (`Ref.Root`). No run emits these
     today (finding 7), so this is new work: emit them from the knowledge
     the service run already loads, deduplicated by ID across services,
     not by running the extractor on `pkg/`.
2. **On every access rule fact, `checks`**: the IDs of the checks its
   guard calls, sorted, joined by `,`.
   - The extractor already has them: each `guard.subject`, and the calls
     in `guard.cond` that `checks.has` accepts.
   - Rule kinds without a guard call carry their marker instead: `open`
     (`acl.OpenAccess`), `todo` (`acl.TODO`).
   - A guard inside a range loop (`perItem`) or a visibility filter
     carries `scope_of_check: item` (finding 12). It doesn't reject the
     call.
3. **On every access rule fact, `check_args`**: per check call, in order,
   the operands the condition notation drops (finding 3). Written as a
   JSON list, e.g. `[{"check":"acl.ActorHasPermission",
   "capability":"CanBanUsers","matcher":"acl.UserScope","subject":"kaid"}]`.
   A check is named, not referenced by fact id: `-all` consolidation
   renames ids per service. Per call:
   - the capability, by the Go name of the `capabilities.X` passed. Not its
     value: the extractor parses without types and skips `generated/`,
     where each service's capability variables live; the value is joined
     from the Go extractor's `uses_value` facts or admin's capability
     facts;
   - the scope matcher, as the matcher's own identity (`acl.AnyScope`,
     `acl.GlobalScope`, `acl.UserScope`, `acl.CountryScope`, or a
     service's own matcher), never a guessed meaning (finding 9);
   - the subject argument of a self or coach check (`acl.IsCurrentUser`,
     `acl.IsCoachedByActor`), as the source text of the argument and,
     when it is a resolver parameter, the parameter's name;
   - for a marked wrapper (`actorHasGlobalPermission`), the wrapper as
     the check and its capability argument, with no matcher: its scope
     lives in its body (a loop testing `scope.IsGlobal()`), not in an
     `acl` matcher argument, so it can't be read from the call. A
     wrapper's meaning is pinned as data in the export vocabulary, or
     its guards aren't exported;
   - for a rate limit, its count, window and key (from what
     `ratelimit.go` already reads for the sentence). The inventory shows
     them; the export leaves rate limits out (Part 3, R3).

   It's a separate attribute, so the condition notation and its hash stay
   as they are, and no golden condition changes.

   This closes the scope and rate-limit half of the `TODO.md` item "Have
   the webapp extractor's conditions carry what its EARS text states",
   by carrying the values beside the condition instead of in it. For it
   to count, the condition-disagreement check (`grounding.Annotate`,
   `condition_disagreement`) also treats a value found in `check_args` as
   carried. The 2026-11-04 measurement found 298 extractor rules that
   disagree only because the condition leaves out a scope or a rate
   limit; those should stop being flagged. The related item "Narrow
   `condition_mismatch`" (490 rules whose values sit only in a variable
   path, such as `caller.hasPermission.CanX`) gets the same structured
   values to compare against.
4. **A fact per resolver the extractor saw with no recognized check and
   no marker** (`unguarded`, cited to the method). Today these get no
   rule at all, so the inventory couldn't tell "unguarded" from "not a
   resolver". It isn't a rule fact, since it asserts nothing about
   behaviour: it has its own kind, so neither `graph.ValidateRuleFacts`
   nor the EARS check is involved.
   *Measured (2026-11-06):* the first count found 54, all a field of one
   resolver type served by another's method on a receiver built in place
   (`(&classroomResolver{r.Resolver}).X`). Those are now delegations,
   checked as their target when it has a rule (`delegates_to`), as a
   same-type rebuilt receiver already was; 0 unguarded remain in the seven
   services (`docs/verification/policy-checks-2026-11-06.md`).

**Go extractor facts (what a check reads).** These are emitted for every
function, like `call`, because which functions are checks is the webapp
extractor's knowledge, joined in the core.

5. **`reads`**: a function's selector reads of a struct field of a module
   struct type (`pkg.Type.Field`, the names the `struct` facts already
   use).
   - Types come from `go/types` (the extractor type-checks), so
     `udi.PrimaryRole` resolves to `models.UserDistrictInfo.PrimaryRole`
     whatever the variable is called.
   - Generated files are excluded, as for `struct` facts.
6. **`uses_value`** (replacing the earlier `uses_constant`, which would
   have found no capability; finding 6): a function's references to a
   package-level `const` or `var` of the module or of a ref root, with
   the value's id, its declared type, and its literal value when the
   initializer is a basic literal or a conversion of one.
   - Generated files are included for this fact, because the capability
     variables live in them.
   - Kinds are then read off the declared type: `permissions.CapabilityName`
     or admin's `capabilities.Capability` is a capability,
     `featureflags.FeatureFlagName` is a flag.
7. **`loads`**: a function's datastore loads (finding 8).
   - The kind: the model type of `datastore.NewQuery(<Kind>)`, of a
     `Get`, or of a loader built with `loaders.LoaderForQuery`.
   - The property names it filters by (`FilterField("kaid", …)`, the
     loader's key argument), mapped to fields through the `datastore:`
     tags.
   - That needs `struct` facts to record each field's tag: a new
     attribute, `tags`, beside `fields`.
8. **Calls through package-level variables** (finding 8), after `FIX.md`
   R3 and separate from it:
   - `FIX.md` R3 lands first, as written: every package-level `var` of a
     non-test file becomes a target of `test` facts, and `SymbolFacts`
     and call facts stay unchanged.
   - This change then knowingly reverses that last point, for a
     narrower set: a package-level `var` whose initializer is a call or
     a function literal becomes a definition call facts can name. A use
     of `udiLoaderByKaid` is a call of it, and its initializer's
     references (`_uncachedUserScopesForCapability`) are its calls.
   - Both use one helper that indexes package-level `var`s by the
     position of their name, written for `FIX.md` R3 and reused here.

   Call facts feed `callers`, drift's callees, journeys and test reach,
   so this change has its own measurement before adoption: call facts
   added, journey lengths, drift `affected` rules, and `callers`' domain
   placements, before and after.
9. Request-context predicates need no new fact. They are calls
   (`ktx.RequestIsServiceAdminQuery`, `RequestIsTask`), already in `call`
   facts when the callee is in the module or a ref root.

Volume: districts has 16,689 call facts; reads and loads may be several
times that. Measure the facts file size before adopting (step 4 below).
If it's too large, emit reads only for functions reachable from a check.
That needs the webapp facts inside the Go extractor, though, so prefer
emitting everything and filtering in the core.

**Core (`internal/policy`, pure).**

10. **`Build(facts, rules) Inventory`**. For each access rule fact, it
    takes the endpoint (`attrRoute`), the checks and `check_args`. For
    each check it computes the closure through `call` facts to a bounded
    depth (`MaxDepth`, as `testreach`; interface dispatch ignored, as for
    drift), and the union of `reads`, `uses_value`, `loads` and called
    request predicates over that closure.
11. **Kinds, from structure only** (no word lists):
    - `staff`: a capability value (by declared type, item 6) passed to an
      `access_check` of form `acl`, with its matcher from `check_args`;
    - `flag`: a flag value (by declared type) passed to the module's
      feature-flag lookup;
    - `context`: a called request predicate (a method of the request
      context type);
    - `self`: the `acl.IsCurrentUser` check with its subject from
      `check_args`. A structural definition ("compares a parameter with
      the caller's kaid") would need a fact no extractor emits. So
      `self` is the one named check, pinned in the package doc, as the
      extractor's own `callerKaid` special case already is
      (`authz.go:451`);
    - `load`: a `loads` entry, with its kind and filter fields;
    - `field`: every other read, with its struct and field.

    The split of `field` and `load` into relation, edge attribute, or
    user or resource attribute is the registry's to say (next point), not
    the inventory's. A field holding kaids is a relation only because the
    registry maps it to one.
12. **Registry comparison**: an optional YAML a person writes (like
    `targets.yaml`, never written by codetospec). It maps each registry
    property and relation to what it's built from:
    - fields, `models.ClassroomDistrictInfo.AllTeacherUDIsIDs`;
    - methods, `models.UserDistrictInfo.DirectlyAdministersSchools`;
    - kinds for existence properties, `hasUMI: load
      models.UserMetaInfo by kaid`;
    - and a free-text `condition` that the inventory shows but never
      evaluates (finding 10: "when `!IsGlobalAdmin`", "minus grantees").

    For example, `admin_of: [models.UserDistrictInfo.DirectlyAdministersSchools,
    models.UserDistrictInfo.DirectlyAdministersDistricts,
    models.UserMetaInfo.DirectlyAdministers]`. A read inside a declared
    method counts as declared. Each `field` or `load` input is then
    `declared` (by which entry) or `undeclared`. "Declared" means the same
    source, not the same meaning: the output says so. `staff`, `flag` and
    `context` inputs are reported by kind, since the datamodel puts staff
    out of scope (its D10) and has no flags.

    The comparison also lists the registry entries that no check reads.
    The PIP should keep, and send each PDP, only what policies need
    (authz §6.11).
13. **Descriptions**: for each check, the LLM rules whose citation
    overlaps its definition (as the manual pass did), so that a person
    reads what the check decides beside what it reads. Their verdicts
    come along; a refuted rule is marked, not hidden. These are LLM
    rules, so they can change between remaps; everything else in the
    inventory is deterministic.

**Output.** A command, `codetospec policies --out <graph> [--registry
file] [--format md|json]`.

- It reads `graph.json`, the nodes and the merged
  `.codetospec/facts.json`, and writes to stdout (like `diff`); it never
  writes the output directory.
  - Not the raw extractor files (`webapp-extractor.facts.json`,
    `go.facts.json`): the pipeline writes `facts.json` from the merged
    facts after its filters (`cmd/codetospec/pipeline.go:226`), so one
    filter applies to every reader.
  - `FIX.md` R1's citation filter (`source.Excluded`) covers this plan's
    new kinds too: `access_check`, `unguarded`, capability and matcher
    facts, and `reads`, `uses_value` and `loads`. Today only admin's
    `testutil` registers anything (one role), but a test helper that
    registered a capability or marked a checker would otherwise feed the
    inventory and the export.
- Markdown: per service, the endpoint table, then per check its inputs
  and descriptions, then the undeclared inputs summed, then the `open`,
  `todo` and `unguarded` lists.
- A "bypasses" summary per service: the guards that a service-admin
  request (306 access rules across the seven outputs), a task request (51)
  or an Okta-lifecycle request (4) passes without a per-resource
  decision. authz §1.3 and §7.5 ask that automation go through the same
  policy as people, so these are the decisions that don't.
- JSON, for joining across services: a script, like
  `scripts/authzall.py`, joins the services, and `scripts/authzview.py`
  can show it.

Considered and set aside: extending `scripts/authzview.py`. It reads the
same facts, but the core rule is deterministic, tested Go, and the reads
and closure belong beside `testreach` and `drift`, which already walk
calls.

### Steps

Each step: tests first (table-driven, stdlib), `make check` in the
module touched, lint fixes without relaxing `.golangci.yml`, then the
measurement, then README and AGENTS.md.

1. **`checks`, `check_args` and `access_check` (webapp extractor).**
   Facts and attributes; goldens regenerated and reviewed;
   `TestExtractorRuleFactsPass` still green. Measure:
   - distinct checks per service against the regex count (59) and the
     backtick count (about 173); every name of finding 3 should appear;
   - per service, the access rules whose every check has its operands in
     `check_args`. Today only admin's 3 service-admin guards of 44
     translate in full.
2. **`unguarded` facts.** Measure per service: resolvers open, todo,
   guarded, unguarded. Check a sample of unguarded resolvers by hand (the
   17-resolver sample of finding 1 is a start).
3. **Calls through package-level variables** (design 8, after `FIX.md`
   R3). Measure call facts added, journey lengths, drift `affected` and
   `callers`' domain placements before and after; adopt only after
   review.
4. **`reads`, `uses_value`, `loads`, and `tags` on `struct` facts (Go
   extractor).**
   - Tests on a fixture with a model struct with `datastore:` tags, a
     generated capability `var`, a flag `const`, and a dataloader `var`,
     through the real load path. (AGENTS.md's `Tests: true` caveat:
     read facts from the packages without tests.)
   - Measure facts file size and extraction time on districts.
5. **`internal/policy` core.** Pure `Build`, kinds, registry comparison.
   Tests with small fact sets for each kind, and one through the real
   extractors on the webapp fixture.
6. **`policies` command.** Deterministic output (two runs
   byte-identical), and a golden on the fixture.
7. **Validate on districts against the manual pass.**
   - Ground truth: the per-function field, capability and flag lists of
     the 2026-11-06 manual verification (CanViewCDI,
     CanManageDistrictTeacher, ActorCanClearUMI,
     UserAdministersAdminAggregate, UserIsKADSSSuperAdmin,
     VisibleAssignmentDistrictInfos and the rest of `permissions.go`).
   - Report recall and precision of inputs per function. A miss is a
     fact the extractor should emit; a false input means the closure went
     too deep.
   - Then a first registry file from the datamodel's Relations and
     property tables, and the undeclared list compared with the manual
     one (11 items, plus the other services' list).
   - Record in `docs/verification/policy-inventory-<date>.md`.
8. **The other services**: users, coaches, assignments, ai-guide, admin.
   The cross-service checks (`cross_service.IsManagedByActor` calls a
   coaches GraphQL field) end at a `dependency` fact. Join them across
   services by the remote field (the webapp extractor's genqlient parsing
   already names it), in the joining script, not the core.

### Limits to state in the output

- Checks the extractor doesn't recognize are invisible: the inventory is
  complete relative to `checks.has`. The `unguarded` list is where to
  look.
- Only resolver methods are enforcement points; struct-bound fields
  inherit their parent's access.
- A closure stops at `MaxDepth` and at interface dispatch. An input read
  deeper is missed, and the output says the depth.
- Reads through maps, reflection or JSON blobs aren't field reads.
- Cross-service checks are resolved only by the join across services.

## Part 3: policy exports to the Cedar policy language and Cerbos

### Goal

From the same facts, write each service's access rules as policies in
two formats:

- the Cedar policy language: policies, a schema, and an entity-data
  contract;
- Cerbos: resource and derived-role policies, JSON schemas, and a test
  suite.

Each policy is traced to the rule id and citation it came from, and
every access rule that doesn't translate is listed with the reason. It's
a reviewable specification of what the code enforces, not a drop-in PDP.
Grants live in datastore (`UserPermissions`) and site admins in GCP IAM,
so neither format can decide a request without a loader that codetospec
doesn't write.

### Findings

**F1. webapp's permission model** (verified in code at `c55b04b0090`):

- **Capabilities.** 132 are registered in
  `services/admin/permissions/capabilities/` (57 with
  `RegisterCapabilityDefinition`, 75 with `RegisterCapabilityName`), and
  about 52 declare `RequiredScopeKeys`.
  - Each has three spellings: Go name `CanBanUsers`, stored name
    `can_ban_users`, and the GraphQL enum `CAN_BAN_USERS` that services
    use through their `generated/capabilities` (finding 6).
- **Roles and grants.** 46 roles bundle capabilities (`FIX.md` F1).
  Users hold grants of (role or capability, scope map) in
  `UserPermissions`.
  - A role grant gives its capabilities at that grant's scope
    (`services/admin/permissions/scopes.go:169-185`); an unregistered
    role is skipped with a warning.
  - A user with no grants gets no scopes at all
    (`HasAnyPermissions`, `has_any_permissions.go:52-73`).
- **Site admins** get every capability at global scope
  (`scopes.go:123-129`). They're the members of two GCP org IAM roles
  (`rolesThatAreSiteAdmins`, `admin.go:73`) plus a hardcoded fallback
  list (`admin.go:57`), read through `IAMClient`, a GCP IAM policy
  reader. No code fact can see who they are.
- **A capability check is (capability, scope matcher).**
  - Call sites in `services/` and `pkg/` outside tests: `acl.AnyScope`
    270, `acl.UserScope` 127, `acl.GlobalScope` 108, `acl.CountryScope` 1.
  - `UserScope` needs a runtime lookup, and local matchers differ
    (finding 9).
  - Other services check through `pkg/khan/acl`, which asks admin over
    GraphQL. Only admin reads `UserPermissions` itself.
- **Single capabilities are the house style.** `pkg/khan/acl/permissions.go:90-98`
  calls OR-ing capability checks an anti-pattern ("create a new
  capability, and grant it to all relevant roles").
- **Other checks:**
  - login, with or without phantom users;
  - self (`acl.IsCurrentUser`);
  - coach (`acl.IsCoachedByActor`, a runtime lookup);
  - service-admin, task and Okta-lifecycle requests;
  - header secrets.
- **A service-admin request is one shared secret.** It's the header
  `X-Ka-Service-Admin-Query`, validated against a single
  `ServiceAdminQuerySecret` (`pkg/web/service_admin_query.go:13-27`). Any
  service holding the secret is service-admin, and the request carries no
  identity for the calling service (authz §6.4, §7.5).
- **Impersonation runs every check as the impersonated user.**
  - `pkg/khan/acl` asks admin's `actorCapabilityScopes`, which uses
    `ktx.RequestUser()` (`services/admin/resolvers/scopes.go:49`): the
    impersonated user.
  - The support person appears only in `UserIdentity.ImpersonatorKaid`
    (`pkg/web/user_identity.go:48-51`).
  - This is by design: the `Impersonator` role "can only impersonate
    (non-elevated) users" (`permissions/roles/support.go:118-121`).
  - At least one access check reads the impersonator: districts'
    `recordAdminActivity` rejects any request made while impersonating
    (`resolvers/admin_activity.go:47-51`).
  - authz §5.12 asks that the acting principal be explicit in policy.
- **Granting is gated by capability, not per role.**
  - `GrantingCapability` and `CanGrantAllCapabilities` are documented
    (`permissions/roles/role.go:24-25`), but nothing reads
    `.GrantingCapability`. They aren't enforced, so they mustn't be
    exported.
  - The content-role mutations check `CanGrantContentRoles` and a
    whitelist of grantable roles (`_validContentRoleNames`,
    `permissions/management.go:24`).
  - `revokeAndGrantRoles` requires a service-admin request or
    `CanChangeUserRoles` (`resolvers/revoke_and_grant_roles.go:88-90`).
    No role grants that capability, so only site admins, direct
    capability grants and service-admin callers reach it.
  - So authz §5.8 (gate granting separately from use) holds in practice.
    The grant mutations' guards are exported like any other.

**F2. What codetospec carries today** (admin, 2026-11-05):

- **44 extracted access rules.** 9 have no condition, and 9 have
  conditions entirely in the authorization vocabulary. Of those 9, only
  the 3 service-admin guards translate faithfully:
  - 5 permission checks have their scope only in the sentence
    (`acl.GlobalScope`);
  - 1 is an inherited gateway-secret middleware rule.
- **Wrapper checks** are opaque variables: `actorHasGlobalPermission` 11
  times, `actorHasUserScopedPermission` 4 (finding 3).
- **Role facts carry `role` and `capabilities`**, but `ruleFactExtra`
  copies only ears, nature and origin (`internal/graph/rulefacts.go:383`),
  so the graph keeps the capabilities only in the sentence. There are no
  capability facts.
- **`graph.json` carries no condition or facet.** The frontmatter carries
  `condition`, `facets: access` and `exposed_by`, and `render.LoadDir`
  reads it.
- **Exporters are pure, pinned-format packages** (`internal/quire`,
  `internal/kiro`). `codetospec export --out <graph> --to <empty dir>`
  reads one graph.
- **`FIX.md` R1 comes first.** Until it lands, the `testutil` role would
  be exported as a real role.

**F3. The Cedar policy language** (cedar-docs; cedar-go `7f6b40e`):

- **Decision rules:** deny by default; a satisfied `forbid` overrides
  any `permit`; a policy that errors is skipped.
- **Scope** constrains principal, action and resource with `==`, `in`
  (transitive over `parents`) and `is`. Conditions go in `when` and
  `unless`.
- **No quantifiers:** sets support only `contains`, `containsAll` and
  `containsAny` of whole values.
- **Entity tags** take computed string keys (`getTag`). "Now" comes only
  from the request `context`.
- **cedar-go** (module `github.com/cedar-policy/cedar-go`, Go 1.26,
  three dependencies: `go-json-experiment/json`, `go-cmp`, an old
  `golang.org/x/exp`):
  - a stable `ast` builder, `MarshalCedar` and `MarshalJSON`, and
    `cedar.Authorize`;
  - a schema parser and a strict validator in `x/exp/schema` and
    `x/exp/schema/validate`, both marked unstable;
  - no templates (per its README);
  - `PolicySet.MarshalCedar` drops policy ids, so emit `@id`
    annotations.
- **Consequence for scope.** "Holds a global grant of X" can't be read
  from a set of grant records. The entity loader has to pre-flatten it
  into groups, e.g. `CapGlobal::"can_x" in CapAny::"can_x"`. And because
  a role's capabilities take the grant's scope, role → capability edges
  are only static data when scope is ignored. So an export in the Cedar
  policy language is policies + schema + a written **entity-loader
  contract**.

**F4. Cerbos** (`~/Documents/git/cerbos`):

- **Policies:** YAML resource, derived-role, principal and role policies
  (`api/public/cerbos/policy/v1/policy.proto`), with CEL conditions.
  - CEL's `exists()` over `P.attr.grants` expresses the scope matchers
    directly, and `R.attr` carries runtime facts such as
    `kmapDistrictOwned`.
- **Decision rules** (`docs/modules/policies/pages/evaluation.adoc:40-52`):
  deny by default; for one role, DENY beats ALLOW; across roles, any role
  that ends at ALLOW wins. So a guard must be a DENY rule with
  `roles: ["*"]` (`best_practices.adoc:143`) to bind every role.
- **Go types** are in the separate module `api/genpb`, which needs
  connect, grpc, grpc-gateway, vtprotobuf and genproto. All parsing,
  validation and compilation is under `internal/`.
- **Validation:**
  - in-process: only structural, against
    `schema/jsonschema/cerbos/policy/v1/Policy.schema.json`;
  - full: `cerbos compile <dir>`, which also runs `*_test.yaml` suites.
- **Unverified:** whether `cerbos-sdk-go` has a policy builder. It isn't
  in the module cache.

### Recommendations

**R1. One authorization model, two renderers.**

- Extend `internal/policy` (Part 2) with a pure `Model`. It holds:
  - operations (endpoint, actions);
  - guards (rule id, citation, checks with `check_args`);
  - capabilities, roles, matchers and principal types.
- A decision never depends on guard order (authz §3.1): every guard must
  pass. The model keeps the resolver's order only to say which error a
  request gets.
- `internal/cedarpolicy` and `internal/cerbos` render it, each pinning its
  format in the package doc as `quire` and `kiro` do.
- The model is where translation succeeds or fails, once, so the two
  outputs never disagree on which rules they cover.

**R2. Principals, resources and context.**

- Principal types: `User`, `Anonymous`, and `Service`.
  - Service-admin and task callers are `Service` principals with their own
    permits, so their bypasses are visible policy instead of context
    flags (authz §7.5). The export notes that service-admin is one shared
    secret with no caller identity (F1).
  - Okta-lifecycle requests are request context.
- Impersonation: the principal is the impersonated user, which is what
  the code checks. The support person is `context.impersonator`, set from
  the verified session (authz §5.12). The pinned vocabulary gets
  `caller.isImpersonated`, because `recordAdminActivity` denies on it.
  The export lists impersonation as a finding: every other check can't
  tell a support person from the user.
- Resources: the user a check targets (from `check_args.subject`), with
  the runtime attributes `kmapDistrictOwned` and `coachedByActor`.
- Site admins: a `site_admin` group that holds every capability
  globally, flagged as fed from GCP IAM.
- Staff capabilities as the datamodel's D10 settles them. Until then, as
  principal data the loader contract names.

**R3. Translate guards one to one.**

- Each guard becomes one deny carrying its rule id, written as `unless`
  the check holds:
  - Cedar policy language: `@id("<rule id>") @source("<path:lines>")
    forbid (principal, action == Svc::Action::"Mutation.x", resource)
    unless { … };`;
  - Cerbos: an `EFFECT_DENY` rule with `roles: ["*"]` and the negated
    check as its CEL condition.

  Why a deny per guard, rather than one permit per operation whose
  condition joins every guard:
  - `forbid … unless` fails closed when an input is missing (authz §3.3);
  - a deny names the guard responsible (authz §11.1, §5.17). In cedar-go,
    a default deny (no permit matched) returns no reason at all.

  Every optional attribute a check reads is guarded with `has`
  (authz §3.3).
- Each operation also gets a permit, scoped as narrowly as the code
  allows: one per principal type the operation admits (`principal is
  Svc::User`, a separate `Svc::Service` policy; authz §5.4), and the
  resource type when the check names one. In Cerbos, which has no
  principal types, that's an `EFFECT_ALLOW` rule per principal kind,
  given as a role the loader sets (`user`, `service`, `anonymous`).
  - authz §5.1 (MUST) forbids blanket permits. The breadth left after
    scoping is the code's own default: the resolver runs unless a guard
    rejects.
  - The export reports that breadth per operation as a finding instead
    of hiding it.
- This matches the code: guards are top-level (finding 12). The export is
  only as complete as guard detection, so it lists `todo` and
  `unguarded` operations explicitly, never silently allowed.
- Not translated, and listed with a reason:
  - per-item and visibility guards (`scope_of_check: item`);
  - rate limits and header contracts (not authorization);
  - policies (`rulePermit`) until measured;
  - LLM access rules (118 in admin): never exported as policy (authz §14.1,
    §14.3). They're offered as candidates that a person adopts by hand,
    with a permit test and a deny test (authz §14.4);
  - any guard with a check outside the pinned vocabulary.
- How a person adopts a candidate: by writing it as a rule fact in a
  person-owned facts file (for example `<out>/adopted.facts.json`, passed
  with `--facts`; `scanwebapp.sh` needs a small change to pass it when
  present), with a
  condition and `check_args` in the pinned vocabulary. It's validated by
  `graph.ValidateRuleFacts`, rendered with `extracted_by` set to the
  file's origin, and the export lists it as written by a person.
  - Not by editing the LLM rule's file into a reviewed rule (`status:
    reviewed`). `render.KeepReviewed` keeps a person's file only in place
    of a rule the run produces under the same id, and drops it otherwise
    (`internal/render/reviewed.go:167-196`). The reduce rewrites
    requirements freely (`docs/verification/remap-2026-11-05.md` §1),
    so an id survives a remap only through citation overlap.
  - Reviewed access rules that do exist are exported with their
    structured fields as they stand, and listed as reviewed.
- No LLM output decides what's exported: a crosscheck verdict may annotate
  a policy, never include or drop it (authz §14.1). Extractor rules carry
  `crosscheck: mechanical` today; this keeps it so if the export ever
  widens.
- `open` becomes the blanket allow alone; `todo` becomes the allow with an
  `@unchecked` annotation (a Cerbos rule name).
- Inherited middleware rules (`shared-*`, such as the gateway secret and
  login) apply to every operation of the service. They're rendered once
  per service as a forbid with `action` unconstrained (Cerbos:
  `actions: ["*"]`), not repeated per operation (authz §3.5, §3.6), or
  left out; the output says which.

**R4. Scope matchers by identity.**

- A guard translates only if its matcher is in the pinned list:
  `acl.AnyScope`, `acl.GlobalScope`, `acl.UserScope`, `acl.CountryScope`.
  Each has a template per format, written from the matcher's code:
  - Cerbos: `P.attr.grants.exists(g, g.capability == "<c>" && <scope
    test>)`, where `UserScope` is `g.scope.userKind == "all" ||
    (g.scope.userKind == "nwea-map" && R.attr.kmapDistrictOwned)`;
  - Cedar policy language: `principal in CapAny::"<c>"`, `principal in
    CapGlobal::"<c>"`, and for `UserScope` `principal in
    CapGlobal::"<c>" || (principal in CapNwea::"<c>" &&
    resource.kmapDistrictOwned)`. The loader contract states how
    membership is computed from grants, roles (at their grant's scope)
    and site admins.
- A service's own matcher, such as districts' `_checkActorCapabilityScopes`,
  isn't translated until its semantics are pinned too.

**R5. The registry types the attributes.**

- The datamodel generates the PDP's schema from its registry, and fails
  a policy that reads an undeclared property. So the exporters take the
  same `--registry` file as `policies` (Part 2, design 12).
- The Cedar policy language schema's entity attributes and the Cerbos
  JSON schemas come from it. A guard that reads an undeclared input
  isn't exported; it's listed.
- codetospec invents no attribute names.
- Schema conventions, pinned in each package doc:
  - one namespace per service, `Districts::`, `Admin::` (authz §4.6);
  - actions named after the operation, `Svc::Action::"Query.<field>"`,
    `"Mutation.<field>"` (authz §4.5);
  - each action's `appliesTo` lists its principal and resource types
    explicitly, never `Any` (authz §4.3);
  - request context fields (`impersonator`, `isOktaLifecycle`) declared
    in the schema (authz §4.4).

**R6. Formats, dependencies and validation.**

- **Cerbos:** your own Go structs → `yaml` (already a dependency). Tests:
  - a unit test validates every file against a vendored
    `Policy.schema.json` (revision noted in `testdata`);
  - the exporter also writes a test suite (R7);
  - a tagged test (`cerboslocal`, like `phplocal`) runs `cerbos compile`
    on the output when the binary exists.

  Don't take on `api/genpb`'s grpc stack.
- **Cedar policy language:** `cedar-go` gives the in-process validator
  and `cedar.Authorize`, so `go test` can check the export. Adding it to
  the main module's locked dependency set is the user's decision, as
  indomain was. Pin the version, since the validator is in `x/exp`.
- Static policy analysis (conflicts, unreachable rules; authz §9.9)
  needs the Rust Cedar policy language CLI; cedar-go has none. It's
  optional, run outside `go test`.
- The two formats need the same R1–R5 inputs. If only one ships first:
  - Cerbos when scope must be stated exactly, since CEL quantifies over
    grants;
  - the Cedar policy language when in-process validation matters more
    and the loader contract (F3) is acceptable.

**R7. Verify the translation mechanically.**

- For each exported guard, enumerate the truth assignments of its checks
  (`condition.Evaluate`, the 2ⁿ idea of `IMPROVEMENTS.md` #13).
- Assert the same allow or deny from `cedar.Authorize` on generated
  entities, and from the generated Cerbos test suite (principals,
  resources and expected effects).
- Every allow the enumeration asserts is paired with the boundary deny
  of the same guard (authz §9.6); the enumeration produces both.
- Optionally, the generated tests also assert `FIX.md` F6's two
  invariants, which only webapp's tests state today: any action
  `kai_supporter` allows, `supporter` allows; the `impersonator` role
  allows only impersonation.
- Record it in `docs/verification/`, as the Kiro and Quire exports were.
- This verifies the translation, not the code. Rule-to-code fidelity
  remains the extractor's job.

**R8. Combine services last.**

- admin defines capabilities and roles; the other services enforce them.
  A combined policy store needs several graphs:
  `export --out admin --out users …`, or admin as an inherited spec.
- A combined store is evaluated as one merged set in which any forbid
  overrides any permit (authz §3.2). The per-guard forbids of R3 compose
  that way without change.
- Do it after single-service exports are verified.
- Optionally, report near-duplicate roles, by the overlap of their
  capability sets (authz §2.3: role explosion).
- `IMPROVEMENTS.md` #13 (decision tables in GoRules JDM) needs the same
  model, so `internal/policy.Model` serves it too.

### Steps

1. **Capability facts (webapp extractor, run on admin).** One per
   `RegisterCapability*`: Go name, stored name, `RequiredScopeKeys`.
   Plus a `matcher` fact per pinned `acl` scope matcher, from the shared
   checks of design 1. Goldens reviewed.
2. **Carry roles through the graph (core).** `role` and `capabilities` as
   frontmatter extras declared in `frontmatterExtras`, copied by
   `ruleFactExtra`. Render goldens regenerated (`UPDATE_GOLDEN=1`).
3. **`internal/policy.Model` (pure).** Built from Part 2's inventory, the
   capability and matcher facts, and the registry; every rule either in
   the model or in its `Skipped` list with a reason.
   - Tests per R3 case.
   - Measure per service: access rules translated vs skipped, by
     reason. admin is 3 of 44 today; step 1 of Part 2 should raise it.
4. **`internal/cerbos` and `--format cerbos`.** Pure `Export(model)
   Spec`, like `kiro.Export`.
   - JSON-schema test, generated test suite, `cerboslocal` compile test.
   - A golden on the webapp fixture.
5. **`internal/cedarpolicy` and `--format cedar-policy`** (after the
   dependency decision). Policies, schema (from the registry), and the
   entity-loader contract as a README in the output.
   - Validator test, `cedar.Authorize` test, golden.
6. **Equivalence measurement (R7)** on admin and districts, recorded in
   `docs/verification/policy-export-<date>.md`.
7. **Several graphs (R8).** `export` accepting several `--out`, or an
   inherited admin spec.
8. **Docs.** README (one paragraph per format, with the CEDAR naming note
   from the top of this plan), AGENTS.md (the two packages, the pinned
   vocabulary, the loader contract).

## Order and size

1. **Part 1 first:** one attribute, one measurement, and it gives every
   resolver its journey, which Part 2's endpoint → check view can use for
   checks outside the guard.
2. **`FIX.md` R1** (exclusions) before any export. **`FIX.md` R3** before
   **Part 2 step 3**, which reuses its helper and then changes call facts
   (design 8).
   - Of these, only `FIX.md` R1 moves the corpus (admin loses its leaked
     `testutil` rule), so it needs one reviewed re-record. `FIX.md` R3
     and Part 1 move nothing the corpus counts. Once `FIX.md` R3 lands,
     the inventory can show each check's `tests` count, a lead for
     authz §9.7 (every major rule has at least one test).
3. **Part 2 steps 1-2** already give a complete list of enforcement
   points, their checks and the operands of each check. Steps 4-7 add
   what each check reads and the registry comparison. Do `loads` with
   `reads`, before step 7's recall measurement, or the recall mostly
   counts what `loads` would have found.
4. **Part 3** after Part 2 step 1 (it needs `check_args`) and step 5 (the
   model extends the inventory). Cerbos needs no new dependency; the
   Cedar policy language waits for the cedar-go decision.
