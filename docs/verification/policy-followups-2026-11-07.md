# Policy follow-ups: the caller's kaid, call-level filters, path coverage

Four follow-ups from `checker-definitions-2026-11-06.md`, each measured
on the re-run graphs. Never over-permit stays the rule. (Cerbos and the
Cedar policy language, not CEDAR, Khan's analytics pipeline.)

## Revisions and commands

- codetospec branch `policy-followups` (working tree on `cbf7429`);
  webapp `c55b04b0090`.
- Five scratch copies of spec graphs (users, coaches, assignments,
  ai-guide, districts) were re-extracted with this branch's webapp and Go
  extractors. They were then re-run with `LLM_BASE_URL` on a closed local
  port.
  - The map prompts list the extractor rules, so chunks whose extractor
    rules changed missed the cache and failed: users 34 of 598 chunks,
    coaches 36/581, assignments 17/451, ai-guide 56/2011, districts
    51/850.
  - That costs LLM rules only: the exports read extractor rules and
    facts.
- Exports: `export --format cerbos` and `--format cedar-policy` with the
  review's checkers files (kept outside this repository) and
  `--capabilities-from` admin's graph. `cerbos compile` ran on every
  Cerbos export.
- Baseline: the same graphs exported by `cbf7429` (the previous
  measurement's final exports).

## 1. `User.can*` resolvers and the caller's kaid

- The five districts `User.can*` field resolvers return their check as
  their value:
  - their callees return an error only after a failed load;
  - their rules carry no condition.
  So they are answers, not guards.
- The checkers' real guards are seven call sites that pass the caller's
  own kaid. The review had marked them two-subject.
- The webapp extractor now binds that argument to `from: caller`: a
  `<local>.Kaid` whose local is bound once, at the method's top level,
  from `RequestUser()`. A pin case keys on it with `when_arg: {"N":
  "@caller"}`.
- Two blind readers re-pinned three checkers for `@caller`. One reader's
  form allowed capabilities without the relation the code requires, so
  the stricter reader's form was adopted (`approx`).
- Districts exports 6 of the 7 call sites. The seventh query's argument
  isn't bound to the caller.

**Caught while measuring.** The binding exported ai-guide operations
that the code closes to service-admin requests:
- `RequestUser()` fails on every service-admin request, which has no
  request user. Resolvers return that error before any other guard.
- 8 of 10 caller-bound ai-guide permits admitted Service.

The extractor now reads a branch that returns on that failure (alone,
or as a disjunct of `||`) as a guard on the service-admin check, unless
the path already tests for service-admin. After the change:
- no caller-bound permit admits Service;
- two operations exported before as "allow any caller" are left out
  (their guard also tests a nil user, outside the vocabulary).

`RequestUser()` also fails when a request's identity headers fail
validation. Both loader contracts now say to deny such a request rather
than load it as anonymous.

The first measurement of that change also found a test-suite flaw: the
Cerbos suite realized "service-admin and self" as a service principal
whose id is the subject. `Operation.Principals` assumes that assignment
can't happen and restricts the permit to users, so 7 of 310 assignments
tests failed. The suite now leaves out assignments no single principal
kind realizes (`policy.Realizable`).

## 2. Cross-service join

`policies --join` was run for each graph with every other one as a
target: users, coaches, assignments, ai-guide, districts and admin.

- Across the six graphs the join reaches 2 checks. Both are reported
  "the operation answers no check".
- The join is blocked upstream, not by mapping the user:
  - `dependency` facts name the root field a query starts from
    (`users:Query.user`: 36 of ai-guide's 119), not the federated field
    that decides;
  - several cross-service checkers aren't recorded as checks.
- Next: dependency facts that name the answering field. Whether a person
  pins the user mapping or the extractor reads the GraphQL variables
  waits for that.

## 3. Item filters at call level

The three districts guards that used a visibility filter's failure as a
call-level check:
- **Two were extractor misreads.** A bool field resolver returns `true`
  on the filter's error and on an empty list, so it fails open. A branch
  returning the literal `true` (other results zero) is no longer a guard.
- **The third is a real call-level use:** no data when the caller sees
  none of the user's records.
  - An item entry may now carry `call`, its call-level meaning,
    validated as a part (`Checker.atCall`, the one substitution).
  - It is worded in the statements and compared by `policies
    --compare`.
- The filter is re-pinned `self`/`coach` on the item, with a call-level
  meaning. The three operations the previous re-pin lost are exported.

## 4. Path coverage

- A path's call to a check that has no meaning and no pin now inlines
  that check's own paths (`pathReader`, depth `MaxDepth`, cycles cut).
- The webapp extractor recognizes capability helpers: a function whose
  decision is admin's `hasPermission` field, read from its genqlient
  operation. Calls to one get `means: capability`:
  - the scope comes from the enum;
  - the capability is a literal, a constant passed as the variable, or
    the call's argument.
- content-editing: pins covered by the code's paths went from 1 to 7 of
  12. The rest go through helpers whose locale is an argument, which
  aren't followed yet.

| graph | checks | pinned | coverage computed | covered |
|---|---|---|---|---|
| users | 44 | 10 | 8 | 6 |
| coaches | 27 | 8 | 6 | 4 |
| assignments | 26 | 8 | 3 | 3 |
| ai-guide | 39 | 18 | 15 | 14 |
| districts | 45 | 28 | 13 | 5 |
| content-editing | 32 | 21 | 12 | 7 |

Districts computes one coverage fewer than before. The visibility
filter's pin is now an item entry, and its coverage isn't compared.

## Exports

| graph | permits, `cbf7429` → branch | gained | lost | `cerbos compile` |
|---|---|---|---|---|
| users | 118 → 118 | 0 | 0 | 408/408 OK |
| coaches | 158 → 160 | 2 | 0 | 460/460 OK |
| assignments | 114 → 114 | 0 | 0 | 244/244 OK |
| ai-guide | 160 → 168 | 9 | 1 | 511/511 OK |
| districts | 301 → 311 | 11 | 1 | 878/878 OK |

- **Gained:**
  - operations whose checks are about the caller (ai-guide 9, districts
    6);
  - operations whose only guard rejects service-admin through
    `RequestUser()` (coaches 2, districts 3);
  - districts' User fields reached through the call-level filter.
- **Lost, both on purpose:** two operations exported before as "allow
  any caller", while the code rejects service-admin requests and
  requests with no user.
- Fewer Cerbos tests than before. Assignments no principal kind can
  realize are now left out.

## Limits

- The caller binding needs the local bound once, at the method's top
  level. A kaid passed through a helper or reassigned isn't bound.
- A failed `RequestUser()` is read as service-admin. The other way it
  fails (an identity that fails validation) is the loader's to deny, as
  both contracts say.
- Capability helpers that take their locale as an argument aren't
  followed.
- The join waits for dependency facts that name the answering field.
