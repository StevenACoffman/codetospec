# What each guard checks: checks, check_args, access_check, unguarded

Question: once the webapp extractor names the checks each guard calls,
with their operands (`policy_inventory_plan.md` Part 2 steps 1-2), how
much of webapp's access rules does it cover, and are "unguarded"
resolvers real gaps?

## Revisions and commands

- codetospec working tree on `ccc0566` (`extractors/webapp/checkfacts.go`,
  `unguarded.go`, cross-type delegation in `authz.go`); webapp
  `c55b04b0090`.
- `extract -root ~/khan/webapp/services/<svc> -lang en -exclude-dirs
  testutil` for admin, ai-guide, assignments, coaches, districts,
  graphql-gateway and users, into a scratch directory.

## Results

Access rules below exclude role and header-contract rules.

| service | access rules | with `checks` | with `check_args` | `open` | `todo` | no check named | `item` | capability args | capability + matcher | checks named |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| admin | 44 | 39 | 32 | 7 | 0 | 5 | 0 | 21 | 5 | 12 |
| ai-guide | 262 | 242 | 198 | 39 | 5 | 20 | 7 | 25 | 25 | 35 |
| assignments | 203 | 185 | 122 | 39 | 24 | 18 | 4 | 20 | 20 | 23 |
| coaches | 243 | 225 | 196 | 24 | 5 | 18 | 0 | 53 | 53 | 17 |
| districts | 430 | 390 | 242 | 148 | 0 | 40 | 62 | 70 | 70 | 39 |
| graphql-gateway | 23 | 6 | 6 | 0 | 0 | 17 | 0 | 0 | 0 | 3 |
| users | 308 | 276 | 223 | 53 | 0 | 32 | 0 | 62 | 50 | 32 |

(Counted before cross-type delegations became rules; see below.)

- 120 distinct check names across the seven services. Every helper the
  plan's finding 3 listed is one: `actorIsTeacherOf`,
  `canGrantAiGuideAccess`, `isUserTeacherOfClassrooms`, `_anyPermission`,
  the `acl_alias.*` family, admin's `actorHasGlobalPermission`.
- Forms: 156 marked, 3 rate limiters, 1 predicate, 1 named like an acl
  predicate. pkg marks its acl checks, so they're known as marked.
- admin's capability arguments: 21, only 5 with a matcher. The other 16 go
  through admin's own wrappers (`actorHasGlobalPermission(ktx, iam,
  capabilities.X)`), whose scope lives in their body, not in a matcher
  argument; `check_args` records the wrapper and the capability, and no
  matcher (`policy_inventory_impl.md`, correction 2).
- admin's rules naming no check are not check calls: a delegation, a
  nil-actor test, two gateway-secret comparisons.

### Unguarded resolvers

- First count: 54 public resolver methods with a schema field and no
  guard (assignments 15, coaches 20, districts 19). All 54 were the same
  shape: a field of one resolver type served by another's method on a
  receiver built in place, `(&classroomResolver{r.Resolver}).X(…)` in
  districts' `studentListResolver`. The extractor already treated a
  same-type rebuilt receiver as a delegation; it now treats another
  resolver type the same way, kept only when the target method has a
  rule, and names the target in `delegates_to`.
- After: 0 unguarded in every service; the 54 are "Access check" rules
  checked as their target. Every public resolver method of these seven
  services has a guard, a marker or a backed delegation.

## Limits

- The capability is a Go name; its stored value is joined later (Go
  extractor `uses_value`, admin's capability facts).
- A check known only by its name (an acl predicate whose package isn't
  parsed) has no citation.
- `unguarded` covers resolver methods; gqlgen fields resolved by struct
  binding have no method and inherit their parent's access.
- The 54 new delegation rules reach codetospec's outputs on the next run:
  the corpus's `rules` counts rise by that much per service.
