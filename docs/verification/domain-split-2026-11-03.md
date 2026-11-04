# Subject matter vs technical, from evidence — 2026-11-03

## Question

`DOMAIN_PLAN.md` (recommendation 4) needs each rule classified as subject
matter or technical before any business-concept grouping. The LLM's
`nature` cannot do it (pkg's `datastore` domain marks 48 of 105 rules
business). Can evidence already in an output do it, how accurately, and how
much does it rest on where the cited code lives?

## Revisions

- codetospec `9a18cc5`, plus the uncommitted `scripts/domainsplit.py`.
- Graphs: copies of `~/spec-graphs/assignments` (1,743 rules, written
  2026-11-03 10:36) and `~/spec-graphs/coaches` (2,008 rules, 15:31); the
  originals were not touched.
- Business vocabulary: the `*.graphql` files of webapp `88b55ce5c33`
  (`services/assignments`, `services/coaches`).

## Command

```sh
uv run scripts/domainsplit.py <copy-of-output> ~/khan/webapp/services/<svc> <dir>
```

## Method

Per rule, no LLM:

- Subject-matter evidence: a `touches` edge (entity), an `exposed_by` edge
  (endpoint), a non-`local.` condition variable naming a concept, or a
  business word in the requirement (identifiers split from camelCase).
  Business words: the words of the GraphQL type names the service defines
  or extends, entity names and role nouns, minus a stoplist of generic
  words.
- Technical evidence: a technical term (cache, connection, header, log,
  datastore, …), a technical quality attribute (performance, reliability,
  …), or a first action `log` / `panic`.
- Boundary handling (§7.17 of the plan's book): from the EARS components, a
  failure trigger (`fails`, `returns an error`, `unmarshal…`) whose every
  response only propagates, translates, swallows or logs the error, with no
  domain response (not found, invalid input, unauthorized). Technical,
  unless it is an access check.
- A rule with both kinds of evidence is decided by its trigger and
  conditions (WHERE/WHILE/WHEN/IF): technical words or operational limits
  (cap, page size, batch, cursor, token) there make it technical, business
  words subject; both → question. Technical words only in the response are
  implementation detail of a business rule.
- `verdict` then uses location for what is still open (pkg citation and no
  structural evidence → technical); `verdict_nl` never uses location.

## Result

| | assignments | coaches |
|---|---:|---:|
| subject | 1,132 | 1,288 |
| technical | 547 | 594 |
| question | 64 | 126 |
| technical in service code | 19 | 31 |
| subject in pkg | 53 | 45 |
| of which boundary handling in service code | 17 | 19 |
| access facet | 282 | 346 |
| disagree with LLM `nature` | 302 (17 %) | 326 (16 %) |
| questions without location (`verdict_nl`) | 186 | 255 |
| verdict = "service → subject, pkg → technical" | 1,607 / 1,679 | 1,806 / 1,882 |

Hand-checked by the author (not a domain expert), random samples:

| version | sample | correct |
|---|---|---:|
| first (location decides mixed rules) | 40 assignments | 35 |
| final | 40 assignments | 37 (1 borderline) |
| final, after the coaches stoplist | 20 assignments + 20 coaches | 39 |
| final, by stratum (10 each, assignments) | boundary in service / pkg via location | 10 / 9 |

Errors left: error-code translation (domain validation codes → GraphQL
codes), a read-only client guard in pkg, an operator alert taken as subject.

## Reading

- **Location carries most of it.** In both services the verdict equals
  "service code is subject matter, pkg is technical" for 96 % of decided
  rules; without location, 122 (assignments) and 129 (coaches) more rules
  stay questions. That holds for services that inherit their platform from
  pkg; it says nothing for a codebase whose technical code lives inside it,
  nor for pkg's own output (no `touches` edges, one `exposed_by`).
- **The exceptions are the information.** ~50 subject-matter rules per
  service live in pkg (mastery-level points, content descriptors, email
  suppression, the current-user check): concepts owned by shared code, to
  model as imported contexts. 19–31 technical rules live in service code,
  mostly boundary handling.
- **Layer-named domains hold subject matter.** In assignments, `broker`,
  `resolvers`, `tasks` are 100 % subject, `read` 85 % (64 / 75), `models`
  83 % (15 / 18), the rest questions or 1–2 technical: they must be
  reassigned to concepts, not moved to the platform.
- **`nature` disagrees on 16–17 %,** both directions (datastore keys
  "business"; "publish a student-unassigned event on archive" "technical").
- **Vocabulary from type names needs a stoplist.** Coaches' types made
  "request", "name", "cache", "token" business words until removed; the
  stoplist is hand-made, a limit.

## Limits

- One judge, the author; samples of 10–40. The rates are indicative.
- `touches` and `exposed_by` come from the LLM reduce and the handler
  overlap; they are evidence, not ground truth.
- Regular expressions over English requirements (`--lang en` outputs only).
