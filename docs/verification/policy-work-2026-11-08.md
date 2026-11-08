# Policy work of 2026-11-08: path coverage, rate-limit obligations, checker bugs

The open items of `TODO.md`'s "Policy Inventory and Exports" that could
be built without a new person's decision, plus rate limits, which the
user decided: exported, with the limit as an obligation. Never
over-permit stays the rule. (Cerbos and the Cedar policy language, not
CEDAR, Khan's analytics pipeline.)

## Revisions and commands

- codetospec branch `policy-followups`, working tree on `147ee99`;
  webapp `c55b04b0090`.
- Baseline: the remapped graphs of `remap-2026-11-07.md` under
  `~/spec-graphs`, with `HEAD`'s binary for the exports.
- Six scratch copies (admin, users, coaches, assignments, districts,
  ai-guide) re-extracted with this branch's webapp and Go extractors
  (`scanwebapp.sh` with `DRY_RUN=1`), then run with `LLM_BASE_URL` on a
  closed local port. No extractor rule fact changed, so no chunk
  re-mapped: 0 failed calls, map failures as in the remap (ai-guide's
  known chunk).
- `policies --checkers` and `export --format cerbos | cedar-policy` with
  the review's checkers files (kept outside this repository) and
  `--capabilities-from` admin's graph; `cerbos compile` (0.57.0, the
  release binary) on every Cerbos export.

## 1. Path coverage

16 pins were flagged on the remapped graphs. After the changes below, 9.

| graph | coverage computed | flagged before | flagged after |
|---|---|---|---|
| admin | 3 | 3 | 3 |
| ai-guide | 15 | 1 | 1 |
| assignments | 3 | 0 | 0 |
| coaches | 6 | 2 | 1 |
| districts | 13 → 12 | 8 | 3 |
| users | 8 | 2 | 1 |

- **`any` and `not(any …)` read** (`condition.Disjuncts`: negations pushed
  to the leaves, one alternative per disjunct, capped at 64). A path is
  covered when each of its alternatives matches a pin part: a disjunct
  the pin lacks is a way the code allows that the pin doesn't state.
  Cleared `UserIsKADSSSuperAdmin`, `governingMoa`,
  `actorIsValidActionableInsightRecipient`.
- **An unqualified call within the package** names its check as the pins
  do (`UserCanViewDistrict` is `permissions.UserCanViewDistrict`): before,
  neither its pin nor its paths were found. Cleared
  `UserCanViewAdminAggregate`.
- **The extractor's paths** (`extractors/webapp/paths.go`):
  - a `(*T, error)` checker returning `nil, nil` withholds the value: a
    deny (`_getBooleanSetting`, cleared);
  - a `(bool, error)` checker that denies with an authorization error and
    never by its bool alone decides by its error; the bool is data its
    callers read (`AuthorizeCSVExportRequest`'s `isCurrentUser`, cleared;
    admin's `_canEditAndIsAdmin`'s `isAdmin`);
  - a checker whose first result is a collection is a filter whatever
    follows: its bool is a separate decision
    (`AdministeredSchoolsInDistrict`, no longer compared), and an item
    filter's pin never states that bool where a path tests it.
- **Leaf grounding through a cache wrapper**: the Go extractor marks a
  call of a package variable holding a function (`callee_var`), a hop
  reach doesn't count. The two capability leaves of `GetAllVisibleUDIs`,
  four calls down with the wrapper, went from ungrounded to grounded; no
  flag depended on them.
- **Capability helpers on `hasContentPermission`** (the locale and content
  kind from the call's arguments): a capability check with scope
  `content_args`, which no pin template states, so an operation guarded
  by one stays out of the exports. No graph here has one (content-editing
  isn't remapped).

The 9 left, by cause:

- admin (3): the capability primitive itself. `actorHasGlobalPermission`
  and `permissions.ActorHasUserPermission` compute the capability from
  the grants: a pin of those helpers would cover them.
- districts (3): `_checkActorCapabilityScopes`, `_actorHasNWEAScopedGrant`
  and `_actorCanViewAllAssignmentDistrictInfos` decide in loops, so their
  paths don't read. Pins would cover them (the first two read as
  capability scopes `user_all` and `user_nwea`, the capability from
  argument 1).
- users `aclSetTeacher`: `actor.Kaid == kaid` reads as data, and the pin
  leaves out the service-admin disjunct the code allows.
- coaches `isCoachedByActor`: the coach lookup `isCoachedBy(ctx,
  actor.Kaid, coachee)`.
- ai-guide `isActorDistrictOrSchoolAdmin`: decides in another service.

## 2. Rate limits as obligations

A guard whose every condition variable is a rate-limit call's no longer
keeps its operation out. It becomes an `Obligation` of the operation (the
limiter, key, count and window as written), and the operation is exported
with its other guards. Cerbos: a policy annotation
`obligation:<action>`; the Cedar policy language: `@obligation` on the
permit. Both loader contracts say the enforcement point must apply every
obligation: the policy alone allows what the code throttles. A guard
mixing a rate limit with anything else still doesn't translate, and an
operation left out lists its rate limits with its other guards.

| graph | permits, `HEAD` → branch | gained | lost | obligations | `cerbos compile` |
|---|---|---|---|---|---|
| admin | 37 → 37 | 0 | 0 | 0 | 91/91 OK |
| users | 118 → 142 | 24 | 0 | 27 (24 operations) | 445/445 OK |
| coaches | 160 → 160 | 0 | 0 | 0 | 460/460 OK |
| assignments | 114 → 114 | 0 | 0 | 0 | 244/244 OK |
| districts | 306 → 306 | 0 | 0 | 0 | 878/878 OK |
| ai-guide | 167 → 167 | 0 | 0 | 0 | 511/511 OK |

Every gained operation carries its obligations. Users has 41 rate-limit
guards; the other 14 sit in operations another guard keeps out. Coaches'
3 (one mixed with another check) are in operations kept out the same
way.

## 3. Checker bugs as questions

A checkers entry may record `bugs` (`checker_comment_mismatch`,
`dead_branch`, with the author's detail). `policies` lists each with its
question id, `questions.ID` over the kind, the check, the detail and the
checker's current digest, so a code change reopens it. None is recorded
in the review's files yet.

## Not built, and why

- **`_anyPermission` (22 users sites).** Its checkers reach the resolver
  through a getter: `_getLoginWithPermission(ctx, kaid,
  _anyPermission(c1, c2))`, whose body tests its function parameter. The
  extractor's guard is the fallback "names the check". Reading it needs
  the getter inlined (its parameter's test, with the call's arguments)
  before the combinator can be split; an expansion of a direct call was
  written, found to apply to no real site, and removed.
- **`x.Kaid == y` as self, the coach lookup as coach.** The extractor
  reads guards as calls; a comparison has no call to carry a meaning, and
  rewriting it would change the rule's text and its grounding. 6 guards
  across users, coaches and assignments.
- **A call that only builds the result** (`moaPayload`): the `any`
  reading already cleared `governingMoa`; the call is still named.
- **Cross-service join**: still waits for dependency facts that name the
  answering field, which needs the target service's types.
- **Content-kind re-pins** and the pins named above: private files,
  pinned by two blind readers each; content-editing isn't remapped.
- **The 34 unrecorded checkers**: no build step (recording a helper or a
  steering result as a guard would claim decisions the resolver doesn't
  make).
- **The `pin` command**: deferred by the user.

## Limits

- Coverage is a diagnostic: none of these changes moves an export but
  the rate limits.
- `Disjuncts` drops comparisons other than boolean ones from each
  alternative (they count as data); past 64 alternatives a path doesn't
  read.
