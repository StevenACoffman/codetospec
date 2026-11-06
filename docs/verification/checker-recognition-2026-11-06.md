# Checker recognition and the pin vocabulary

Question: which code shapes kept webapp's checkers out of the inventory,
how many does each extractor change recover, and does the wider pin
vocabulary translate what the checkers files could not say before,
without over-permitting? (Cerbos and the Cedar policy language, not
CEDAR, Khan's analytics pipeline.)

## Revisions and commands

- codetospec branch `checker-recommendations` from `policy-export`
  (`148a972`); webapp `c55b04b0090`.
- Each step was measured by running `bin/webapp-extract -root
  services/<svc> -lang en -exclude-dirs testutil` on the 32 services with
  a `gqlgen.yml`, then counting:
  - (a) checks without a meaning;
  - (b) how many of the 64 checkers a per-checker review had found
    unrecorded are now recorded, as `access_check` names or `check_args`;
  - (c) route facts;
  - (d) rule operations matching no schema field.

  Every count was taken twice, to catch non-determinism.
- Exports: scratch copies of six spec graphs, re-extracted with
  `scanwebapp.sh`, then exported with the review's checkers files (kept
  outside this repository); `cerbos compile` on each Cerbos export.

## Extractor steps

| step | (a) | (b) | (c) | (d) |
|---|---:|---:|---:|---:|
| before | 225 | 0 | 1604 | 3 |
| directive spellings (`// ka:permission-check`, plural) | 227 | 2 | 1604 | 3 |
| the decision result of a multi-value checker; a nil result | 235 | 10 | 1604 | 3 |
| checkers wrapped in a package variable | 236 | 11 | 1604 | 3 |
| helper predicates with local definitions | 237 | 12 | 1604 | 3 |
| functions a denying guard calls, named (`form: call`)¹ | 286 | 22 | 1604 | 3 |
| resolver packages reached by embedding | 301 | 30 | 1646 | 3 |
| only schema fields are operations | 301 | 30 | 1646 | 0 |
| an empty filter that denies is a call check | 301 | 30 | 1646 | 0 |
| a call's decision and failure named apart | 298 | 30 | 1646 | 0 |

¹ As finally built. The first version named a loader whose error a guard
turns into an authorization error (`user, err := Load(…); if err != nil
{ return …Unauthorized }`). That made the export withhold every
operation reaching the loader: one service fell from 112 operations to
88, never over-permitting. A call is now named only where its result
decides (a bare call), or where its error is its only result (`err :=
Validate(…)`).

The shapes behind each step:

- **Multi-value checkers.** `kaid, canSetUp, isActing, err :=
  Checker(…)` decides on its second result, which the scanner didn't
  bind. A checker with no `bool` result denies with a nil one (`key ==
  nil`). Both now name the check.
- **Wrapped checkers.** `var X = cache.Cache(check, …)` hid a marked
  function; any package variable holding a checker, or a wrapper call
  taking it first, is that checker.
- **Named unknown calls.** These raise (a) by 52 on purpose: each is a
  check a person can now pin, where before the guard named none.
- **Embedding.** One service's root resolvers embed per-package resolver
  structs, through type aliases: 6 of its 35 Query and Mutation fields
  were seen, now 35. A first version chose between two directories
  ending alike (`…/resolvers`) in map order; running the count twice
  caught it, and the longest directory now wins.
- **Empty filters.** A filter result tested only for emptiness, to deny
  the call, is a call-level check: rules scoped `item` went from 107 to
  84.
- **Decision and failure.** `if !ok || err != nil` on one call read
  `not X.fails or X.fails`: one AST node bound twice was named after its
  failure in both places. There were 17 such tautologies; now 0. The
  extractor's own goldens had two.

Not built, measured:

- **Aliased imports.** One service, one call; the named-calls step
  already records it once with its arguments.
- **A local any-combinator.** 20 of its 22 call sites pass it to a
  helper, which would need higher-order inlining. The 2 direct sites
  exposed a variable mismatch between the condition and `check_args`,
  which is fixed.

Of the 34 checkers still unrecorded, most decide inside data access or
in another service: no guard holds them.

## Pin vocabulary

The checkers format now says, each test-first and through both
renderers' agreement tests (Cerbos `holds` and suite, the Cedar policy
language `cedar.Authorize`):

- `subject_arg` on a compound checker, passed to its parts;
- `capability_arg`, a part's capability from a call argument;
- `self`/`coach` in an item filter, about the item's user;
- a relation to an argument's user (`subjectRelations`);
- `attribute` of the caller, resource or request, and `not`;
- the `locale` and `explicit` scopes;
- `unpinnable: <reason>` (the skip reason);
- `digest` (`export --src` warns about stale pins; `policies --src`
  prints each check's digest);
- repeatable `--checkers` (files merge, a duplicate name is an error).

The loader contract gained, in each exporter's package doc:

- the principal's `attributes` and `localeGrants`, and a grant's
  `locale` and `explicit`;
- the resource's `attributes` and `subjectRelations`;
- the request's `attributes` and `locale`.

`cerbos compile` on the test model with every new kind: 98 tests, 98 OK.
The Cedar policy language agreement test checks 97 cases.

## Exports

The six scratch graphs, re-extracted (one spent 117k map and 88k reduce
prompt tokens on chunks whose mechanical rules changed; the others
none), exported with the review's checkers files. Before and after this
work, same checkers:

| graph | operations | deny rules | `cerbos compile` after |
|---|---|---|---|
| admin | 37 → 37 | 30 → 30 | 96/96 OK |
| districts | 298 → 297 | 134 → 149 | 1008/1008 OK |
| assignments | 115 → 114 | 58 → 58 | 284/284 OK |
| users | 112 → 112 | 86 → 87 | 446/446 OK |
| coaches | 163 → 157 | 137 → 132 | 520/520 OK |
| ai-guide | 151 → 159 | 111 → 119 | 560/560 OK |

Every Cedar policy language export passes cedar-go's strict validation.

- **Operations lost.** They are withheld because a newly recognized
  check is reached below their guards (`checksBelow`). That is the
  never-over-permit rule seeing checks it couldn't see before. Some of
  them return the decision as data (a resolver answering "is this user
  coached by the caller"), which the rule can't tell from a deeper
  guard. One validator returns an authorization-class error
  (`errors.NotAllowed` for an invalid email), so it reads as a check,
  as its code states.
- **Users with the new vocabulary.** With one entry the old format
  couldn't state (a capability from argument 2, the user from argument
  1, passed to the parts), users exports 118 operations and 93 deny
  rules: 518/518 OK.

## Limits

- Counts (b) are against one review's list of unrecorded checkers.
  Other shapes may hide more.
- `checksBelow` can't tell a check returned as data from a deeper
  guard, so recognizing more checks can withhold operations that were
  exported (over-permitted, if the check was a guard) before.
- An `item` scope remains on a filter whose denial is an error passed
  through from another service: nothing in its code marks the error as a
  denial.
- Content-kind scopes, pins keyed on argument values, two-subject
  operations, the cross-service join, rate limits as obligations and a
  pin-proposal command are not built (`policy_checkers_impl.md` says
  why).
