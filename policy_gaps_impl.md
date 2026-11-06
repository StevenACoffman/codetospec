# Implementation plan: closing the policy-export gaps

Temporary (2026-11-06). Implements the revised gap findings that followed
`policy_inventory_plan.md` Part 3; delete when done (measurements go to
`docs/verification/`). Baseline: the working tree after
`policy_inventory_impl.md`, `make check` green.

## Working rules

As `policy_inventory_impl.md`: comment (Requires/Ensures) before body,
test first, `go test` and `golangci-lint run --fix ./...` in each module
touched (never relaxing `.golangci.yml`), a review pause against
`~/Documents/agent-orange/go-advice/summary_rules.md` after each step,
real-data measurements on scratch copies only, no commit unless asked.

## Steps

### G0. Move webapp knowledge out of the core (prerequisite)

`internal/policy` hard-codes webapp names (`acl.IsCurrentUser`,
`method.Request…`, `caller.loggedIn`, `permissions.CapabilityName`,
`featureflags.FeatureFlagName`, `acl.*Scope`). AGENTS.md: no
repository-specific code in the core. So:

Design it twice (§11):
  (a) a new `meaning` fact per condition variable, matched by name (and
  by prefix for `caller.hasPermission.*`) in the core;
  (b) each recorded check call in `check_args` carries the condition
  variable it became (`var`, `fails` when the variable holds on the
  check's failure) and its meaning (`means`: login, login_strict,
  capability, self, coach, service_admin, task, okta, request; `scope`:
  any, global, user, country for a capability's matcher).
  **Chosen: (b).** The extractor already decides each variable when it
  writes the condition, so the link is exact (no name or prefix
  matching), no new fact kind, and the person-pinned checkers of G2 fill
  `means` for a call the extractor left blank: one translator, one place
  per call.
- Value kinds of `uses_value` (staff, flag): a `value_kind` fact per
  declared type the webapp extractor knows (`permissions.CapabilityName`
  → staff, …), read by the inventory; no type names in the core.
- `internal/policy` keeps only the generic leaf kinds and scope kinds
  whose templates the renderers pin; `Leaf.Matcher` becomes the scope
  kind (`any`, `global`, `user`, `country`).

### G1. Never over-permit; report everything

- **Checks below the guard.** Every rule fact carries its handler
  (`handler_path`, `handler_lines`: the resolver method). `BuildModel`
  follows direct call facts from the handler span (depth `MaxDepth`) to
  the definitions of `access_check` facts; a check reached that the
  operation's guards don't call makes the operation not exportable
  ("checks below its guard: X"). Applies to open operations too.
  Limit stated: checks defined outside `--src` (pkg's acl) aren't
  reachable through call facts.
- **Silent operations.** Every extractor rule with an operation is in the
  model: exported, or skipped with a reason ("guard names no check",
  "delegation to X" until G2 follows it).
- **`scope_of_check`** carried to the graph (`ruleFactExtra`,
  `frontmatterExtras`).

### G2. Pin meanings; follow delegations; authentication as principals

- **Pinned checker definitions**, a person-owned YAML (`--checkers`, on
  `policies` and `export`), mapping a check name to an expression in the
  same notation as `meaning` facts, plus `relation: <name>` leaves (a
  relation the PIP serves, rendered as resource attributes in the loader
  contract). An `X.fails` condition variable of an error-denying checker
  means `not X`. A definition naming a relation the registry doesn't
  declare is reported.
- **Wrappers**: admin's `actorHasGlobalPermission` and siblings pinned in
  a sample checkers file (scratch, and a documented example).
- **Pub/Sub push validation** as a principal kind (`principal: push`),
  from a `meaning` fact the extractor emits.
- **Delegations followed**: rule facts carry `handler` (recv.Method); a
  delegation operation takes its target operation's denies when that one
  is exported whole, else it's skipped with the target's reason.

### G3. Bind subjects to the request

`check_args` records `subject_param` when a self, coach or user-scope
subject is a resolver parameter (or a field of one). The model records,
per operation, the parameters its checks are about; exports write them
in a sidecar `bindings.json` (the PEP's contract: which argument is
`resource.subject`), since neither format has a place for it.

### G4. Per-resource decisions for visibility filters

A visibility guard whose filter has a pinned definition becomes a
per-item rule on the filtered entity's kind (action `view`), exported as
a Cerbos resource policy for that kind and a Cedar policy language
action on that entity type; an unpinned filter stays skipped with its
reason.

### G5. Finish the export

- Shared middleware as one service-wide forbid when its meaning is known
  (gateway secret → `principal: gateway` authentication), else skipped.
- Impersonation: `context.impersonator`, leaf `impersonated`.
- Emit roles and capabilities: Cerbos derived roles per capability
  (parent roles = the roles granting it); Cedar policy language
  `entities.json` of Role groups member of their capabilities' `CapAny`.
- Permits scoped by principal type (one per kind).
- `scanwebapp.sh` passes `<out>/adopted.facts.json` when present.
- Measure the other five services.

## Go-advice review of this plan

- **AGENTS + §16 (coupling):** G0 first: the core couples to a contract
  (meaning facts, generic leaves), not to webapp's names; adding a
  repository is a new extractor table, zero core change.
- **§4 deep modules / §15 avoid special cases:** one expression notation
  for extractor meanings and person-pinned definitions, one translator;
  delegation and checks-below reuse the existing call-closure code
  (`factIndex.reached`), no second walker.
- **§5:** all new logic pure in `internal/policy`; files read in the CLI.
- **§3:** a bad checkers file stops with a message naming the entry; the
  run never guesses.
- **§9-10:** each step test-first, through the real path where AGENTS
  asks (extractor goldens → `graph` contract test).
- **Scope control (§15 YAGNI):** registry-generated schemas and a
  combined multi-service store stay out; they don't change correctness.

## Progress (2026-11-06)

- G0 done: check_args carries `var`, `fails`, `means`, `scope`;
  `value_kind` facts; the core names no webapp function (only comments
  and the scope templates, which encode webapp's grant model by design).
  Review: one translator over call meanings; `carriedValues` skips label
  keys (they name, never carry a value).
- G1 done: silent operations, `checksBelow` (stop set = the guards' own
  checks), `scope_of_check` carried. Review: the op point came from a
  rule id for silent rules, fixed (first guard's endpoint).
- G2 done: checkers file (leaf or any/all, `Deny.Defined` for the
  oracle), push leaf, relation leaf, aliases (`delegates_to_operation`),
  scopes `user_all`/`user_nwea`. Review: design-it-twice on compound
  pins (opaque relation vs expression; expression chosen, exact for the
  checkers read); `readRegistry`/`readCheckers` shared by both commands.
- G3 done: `subject_from` (params, parent, locals defined once),
  `subject_arg`, op skipped for an unbound or double subject; binding as
  annotations (Cerbos rejects a sidecar JSON in its policy dir). Review:
  `Leaves()` dedupes by variable, so `subjectOf` walks every deny.
- G4 done: `item: <Kind>` checkers → `Model.Items`, Cerbos policy per
  kind, Cedar entity type + `view:<Kind>`. Review: item guards carry
  their operation (no hidden field on Operation).
- G5: hand-adoption in `scanwebapp.sh` done; middleware, impersonation,
  role data recorded as not built with reasons
  (`docs/verification/policy-export-gaps-2026-11-06.md`); other services
  measured there.
- Every step: `golangci-lint run --fix ./...` 0 issues in each module
  touched (findings fixed in code, none suppressed); `make check` green.
