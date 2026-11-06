# Policy exports: closing the gaps, on admin and districts

Question: after the first policy exports (`policy-export-2026-11-06.md`),
does each change to the model leave no operation over-permitted, and how
much more of each service do the exports translate? (Cerbos and the Cedar
policy language, not CEDAR, Khan's analytics pipeline.)

## Revisions and commands

- codetospec working tree on `ccc0566` plus the policy inventory and
  export work; webapp `c55b04b0090`; Cerbos `6340b18`.
- Scratch copies of `~/spec-graphs/admin` and `~/spec-graphs/districts`,
  rerun after each step with the remap's flags (`scanwebapp.sh <svc>
  --quality --crosscheck --write-domain-model`, admin with
  `--acceptance`); warm reruns spent no tokens.
- `codetospec export --format cerbos [--capabilities-from <admin copy>]
  --log-level debug` (the skip reasons), then `cerbos compile` on the
  export (it runs the generated suite).

## Step 1: meanings from the extractor; never over-permit

The extractor now states, per recorded check call, the condition variable
it became and what the check means; the core reads those instead of
webapp's function names. An operation is left out when an extractor rule
of it names no check (a delegation, a guard on a local the extractor
can't trace) or when its resolver reaches, within three direct calls, a
recognized check its guards don't call.

| | before | after |
|---|---:|---:|
| admin: operations exported | 18 | 18 |
| admin: deny rules | 11 | 11 |
| admin: guards not translated | 21 | 26 |
| districts joined: operations exported | 224 | 213 |
| districts joined: deny rules | 95 | 88 |
| districts joined: guards not translated | 147 | 214 |

- The translated output is unchanged where it was right: admin's 18
  operations and 22 Cerbos tests are identical.
- New skips are operations that were exported over-permissive before:
  admin 4 guards on untraced locals and 1 delegation; districts 30
  untraced guards, 29 delegations, and one resolver
  (`User.isKmapStudent`) reaching `permissions.GetAllVisibleUDIs` below its
  guard. The earlier estimate of 148 operations with checks below their
  guard counted reach the call facts don't follow; measured through them,
  one.
- `scope_of_check` now reaches the graph: an item guard is skipped as one
  ("drops one item, not the call", 60 on districts) instead of through
  its missing condition.
- `cerbos compile`: admin 22/22 OK, districts joined 276/276 OK.

Limits: reach stops at three calls and at code outside `--src` (shared
`pkg` checks are not followed into), as test reach and journeys do.

## Step 2: pinned checkers, Pub/Sub pushes, delegations

- The extractor gives `pubsub.DecodeAndValidatePushMsg` its meaning: a
  push principal (Cerbos role `push`, a Cedar policy language `Push`
  principal type).
- A delegation names the operation its target serves
  (`delegates_to_operation`); an operation whose every rule delegates to
  one exported operation is its alias (Cerbos rules list both actions;
  the Cedar policy language `action in [...]`).
- `export --checkers <file>`: a person pins what a check the extractor
  doesn't know means, as one check (login, capability on a scope,
  service-admin, push, relation…) or `any`/`all` of them; a checker
  returning an error reads as the check's negation. The generated suites
  decide a compound checker's variable from its pinned parts, so the
  oracle still judges the translation. Two scopes joined the templates:
  `user_all`, `user_nwea` (a grant's userKind, whatever the data), the
  Cedar loader's existing `CapUserAll` and `CapNwea` groups.
- The pins measured below were written from each checker's body at
  webapp `c55b04b0090`: admin's three ACL wrappers
  (`actorHasPermissionWithAnyScope` is any scope, though its comment says
  GlobalScope), districts' five most used checkers
  (`_checkActorCapabilityScopes` accepts userKind "all" only, though its
  callers' comments say "all or nwea-map"). `kadss_superadmin_of` is
  reported as undeclared by districts' registry.

| | step 1 | delegations, push | + checkers |
|---|---:|---:|---:|
| admin: operations exported (+ aliases) | 18 | 18 (+1) | 34 (+1) |
| admin: deny rules | 11 | 11 | 27 |
| admin: guards not translated | 26 | 25 | 9 |
| districts joined: operations exported (+ aliases) | 213 | 225 (+25) | 239 (+25) |
| districts joined: deny rules | 88 | 100 | 114 |
| districts joined: guards not translated | 214 | 177 | 163 |

- `cerbos compile`: admin 24/24 then 92/92 OK; districts 300/300 then
  404/404 OK. The Cedar policy language exports of the pinned models pass
  cedar-go's parse and strict validation.
- Delegations not followed on districts: 29 → 4, each to an operation
  that isn't exported itself.
- Left on districts: 60 item guards (step 4), 30 guards on untraced locals,
  19 checked rules with no condition, `CanManageDistrictClassroom` (its
  subject is an argument, step 3), `UserAdministersAdminAggregate` (a
  feature-flag gate), and the other local checkers, unpinned.

Limits: a pin is a person's reading of a checker's code; codetospec
checks its shape, never its truth. A compound pin may push an operation
past `policy.MaxCaseLeaves` checks, leaving it without suite cases.

## Step 3: subjects bound to the request

Every self, coach and user-scope check reads one `resource.subject`. The
extractor now says where each check's user comes from (`subject_from`:
`arg:<param>[.field]`, `parent[:field]` for a field resolver's object,
through a local the body defines once from either); an operation whose
checks are about a local, or about two users, is left out; the binding is
an annotation on the policies (Cerbos `subject:<action>`, the Cedar
policy language `@subject`). A pinned checker names its user's argument
(`subject_arg`); admin's `actorHasUserScopedPermission` is argument 3.
Admin's two operations withheld at first had their users in locals
copied from the parent object, which following locals restored.

| | step 2 | subjects | + locals, item filter |
|---|---:|---:|---:|
| admin pinned: operations exported | 34 | 32 | 34 |
| districts joined: operations exported | 225 | — | 222 |
| districts pinned: operations exported | 239 | 232 | 237 |
| districts pinned: deny rules | 114 | 105 | 111 |
| districts pinned: checks about an unbound user | — | 9 | 5 |

- The operations now withheld were exported before with a
  `resource.subject` the PEP had no way to fill: subjects read from a
  loaded record (`record.Kaid`, `classroomInfo.TeacherKaid`) or a local
  computed in the body.
- `cerbos compile`: admin 92/92, districts joined 280/280, districts
  pinned 432/432 OK; every Cedar policy language export passes cedar-go's
  strict validation.

## Step 4: visibility filters as item policies

A checkers entry with `item: <Kind>` makes a visibility filter a `view`
policy on that kind (Cerbos resource `<service>.<Kind>`, a Cedar policy
language entity type and `view:<Kind>` action): its operation's call
decision is exported whole, and each item is denied when the filter's
pinned meaning doesn't make it visible (the oracle decides the filter's
variable from the pinned parts). Districts' `VisibleAssignmentDistrictInfos`
was pinned from its body (service-admin or a global grant sees all; an
NWEA grant, the KMAP districts' items, relation `in_kmap_district`):
1 item kind, included in the step 3 table's last column and its Cerbos
suite.

Left: 61 item guards whose filters are unpinned
(`GetAllVisibleUDIs` 18, `AdministeredSchoolsInDistrict` 16,
`FilterVisibleAdminAggregates` 13, `GetVisibleUMIs` 9, …), each keeping
its operation out.

## Not built, and why

- Shared middleware (`web.NewRequestData`, 401 before any resolver): an
  authentication step, the loader contract's precondition (a request
  that fails it never reaches the PDP), not a policy; it guards no
  operation, so it blocks none.
- Impersonation: no check in admin or districts reads it.
- Role definitions as policy data: a role grant carries a scope, so a
  role can't be a static group of capabilities; the loader expands
  grants, as both loader contracts say.

## The other services

Scratch copies of `~/spec-graphs/{assignments,users,coaches,ai-guide}`,
rerun warm with `scanwebapp.sh <svc>` and no judging flags (the export
reads extractor rules only; map spent 23k and 32k prompt tokens on
assignments and coaches, the rest 0), exported with
`--capabilities-from` admin's copy and no checkers file:

| service | operations | deny rules | guards not translated | top unknown checks | `cerbos compile` |
|---|---:|---:|---:|---|---|
| assignments | 102 | 44 | 102 | `isUserTeacherOfClassrooms` 12, `ktx.RequestUser.fails` 9 | 184/184 OK |
| users | 100 | 72 | 185 | `canSeeBioFields` 12, `_actorCanViewUserSettings` 6 | 302/302 OK |
| coaches | 122 | 96 | 130 | (78 unknown checks) | 238/238 OK |
| ai-guide | 113 | 70 | 148 | (98 unknown checks) | 164/164 OK |

Every Cedar policy language export passes cedar-go's strict validation.
The largest share left everywhere is checks outside the vocabulary:
service checkers a person can pin, as admin's and districts' were.

Findings, kept untranslated on purpose:

- `ktx.RequestUser` failing is not "not logged in": it errors only when
  the request carries no user identity, while `acl.IsLoggedIn` also
  refuses pre-phantom KAIDs and service-admin requests
  (`pkg/khan/acl/user.go:32`); mapping it to the login leaf would
  over-permit.
- Two calls of one capability on different scopes in one guard share a
  condition variable (`caller.hasPermission.CanViewUserIdentity`, global
  and any, users' `Query.usersSearch`), so the translator can't tell them
  apart and refuses. Putting the scope in the variable name would change
  every capability condition and its hash (questions reopen, baselines
  move): a separate decision.
- Rate limits (`caller.overRateLimit.*`) are not authorization leaves;
  their guards keep their operations out.
