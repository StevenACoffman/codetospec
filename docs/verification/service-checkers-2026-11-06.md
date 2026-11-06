# Service-local checkers: pinned definitions for every GraphQL service

Question: across webapp's services, which checks do the guards call that
the extractor gives no meaning, how many can a person pin as data
(`export --checkers`) without over-permitting, and how much more do the
policy exports translate once they are pinned? (Cerbos and the Cedar
policy language, not CEDAR, Khan's analytics pipeline.)

## Revisions and commands

- codetospec working tree on `ccc0566` plus the policy export work
  (`policy-export-gaps-2026-11-06.md`); webapp `c55b04b0090`.
- `bin/webapp-extract -root services/<svc> -lang en -exclude-dirs
  testutil` on each of the 32 services with a `gqlgen.yml` (no LLM); a
  check is unknown when its `check_args` entry has no `means`.
- One checkers file per service, written from each checker's body (and
  what it calls, into `pkg/`), each pin citing its code; every file
  passes `policy.ParseCheckers`.
- Exports as in `policy-export-gaps-2026-11-06.md`: scratch copies of
  admin, districts, assignments, users, coaches and ai-guide, joined with
  admin's capabilities, then `cerbos compile` on each export.

## Unknown checks

225 checks, called by 850 guards. Twelve services have no unknown check
other than `web.NewRequestData`, the middleware every service shares.
The other 213 checks:

| status | checks | guards |
|---|---:|---:|
| pinned | 114 | 550 |
| not pinnable: depends on loaded data that isn't a relation (`data_predicate`) | 22 | 42 |
| not pinnable: outside the vocabulary (`other`) | 20 | 85 |
| not pinnable: middleware | 20 | 20 |
| not pinnable: decided by another service | 12 | 49 |
| not pinnable: the user checked isn't the request's | 11 | 23 |
| not pinnable: rate limit | 6 | 52 |
| not pinnable: no authorization decision | 6 | 13 |
| not pinnable: feature flag | 2 | 4 |

Pins are exact except 9 that are stricter than the code: for example,
`login` refuses the pre-phantom users the code admits. None allows a case
the code denies. The pins introduce 32 relations, which the PIP would
have to serve, such as `teacher_of` a classroom or `billing_manager_of` a
subscription.

What the vocabulary lacks, by the guards it would unblock:
- a capability passed in as an argument inside `any`/`all`: users'
  `canSeeBioFields` (13 guards), edu-organizations'
  `actorHasPermissionForCountry` (7);
- a locale scope: 7 of content-editing's checkers (18 guards);
- `self`/`coach` in an item filter, which is about the item: districts'
  and ai-guide's filters fell back to relations to the item;
- attributes of the caller that aren't capabilities (assessments);
- a scope that excludes the implicit grant admins hold: ai-guide's
  `canViewAiGuideHistoryIgnoreAdmin`.

## Exports with the pins

| service | operations | item kinds | deny rules | guards not translated | `cerbos compile` |
|---|---:|---:|---:|---:|---|
| admin | 18 → 37 | 0 | 11 → 30 | 25 → 6 | 98/98 OK |
| districts | 222 → 298 | 0 → 5 | 95 → 134 | 182 → 90 | 626/626 OK |
| assignments | 102 → 115 | 0 | 44 → 58 | 102 → 87 | 284/284 OK |
| users | 100 → 112 | 0 | 72 → 86 | 185 → 171 | 452/452 OK |
| coaches | 122 → 163 | 0 | 96 → 137 | 130 → 86 | 530/530 OK |
| ai-guide | 113 → 151 | 0 → 1 | 70 → 111 | 148 → 106 | 484/484 OK |

Baselines are without a checkers file. Every Cedar policy language export
passes cedar-go's strict validation, once item entity types are named
`<Kind>Item`: ai-guide's filter over `User` collided with the principal
type (`cedarpolicy.ItemType`, `TestItemKindNamedLikeAFixedType`).

What still blocks the most operations, after the pins: unpinnable checks
(users 72 guards, ai-guide 49), rules that name no check (districts 30,
users 31) and checks with no condition (users 33).

## Second pass: verification and completeness

A second set of agents, starting fresh, tried to refute every row,
every pin and every claimed finding, re-reading the code. They also
looked for checkers the extractor never records. The gateways and the
other non-GraphQL services were extracted too.

- Three pins over-permitted; each is now fixed:
  - districts' `FilterVisibleAdminAggregates` and
    `AdministeredSchoolsInDistrict` let deleted records through;
  - progress-reports' `canEditGoal` allowed an empty district id, which
    the code refuses first.
- 64 checkers are unrecorded by the extractor; 23 of them can be pinned.
  The patterns behind them:
  - directive spellings (`ka:permissions-check`, `// ka:permission-check`);
  - unmarked helpers used as guards (ai-guide's
    `actorHasSupportPermissionsForUser`, 16 guards; content-editing's
    `checkEditContentKindPermission`, 18 sites; districts'
    `ActorCanSetUpAndEditAdminUDI`, about 11);
  - checks seen only as condition variables (assignments'
    `ActorCanViewTeacherClassroomOrCanManageDistrictClassroom`, 18
    guards);
  - discussions' resolvers embedded from sub-packages: 8 of about 32
    operations are seen.
- In total: 282 checkers, of which 137 are pinned (17 stricter than the
  code) and 145 are not pinnable, each with its reason.
- Rerun with the revised pins, the exports keep the operation counts
  above. The new pins cover checkers the extractor doesn't record, so
  they don't change the counts. Every suite passes (districts 662/662),
  and every Cedar policy language export validates.
- Of the first pass's notes, four claims were wrong and were corrected.

The per-checker definitions, reasons and pins, and the changes they
suggest for codetospec and the extractors, are kept outside this
repository: they describe webapp's access control in detail.

## Limits

- A pin is a person's reading of a checker's code, here done by an LLM
  agent per service with each pin citing its code. codetospec checks a
  pin's shape and the export's agreement with the rules' conditions,
  never that the pin matches the code.
- Only six services have graphs to export; the other pins are checked
  by ParseCheckers only.
- Non-GraphQL services have no graph, so they were extracted and defined
  but not exported.
- The second pass found what it could confirm quickly; the extractor
  may miss more checkers.
