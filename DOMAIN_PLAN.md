# Deriving business-concept domains — plan

Status: proposal (2026-11-03); partly implemented 2026-11-04:
recommendation 25 (ids survive a domain move), 4, 5, 13, 19 (nature by
evidence, access facet), 7, 10 (api_type and foreign-key seeds), 1, 2
(one proposed concepts model), 3, 12, 15 (concept assignment, recorded
but not applied) and 23 (actors that own concepts are the EARS subject).
Measurements: `docs/verification/{cross-domain-ids,nature-by-evidence,
concept-proposal,concept-assignment,enum-variants}-2026-11-04.md`. Open:
the domain move itself, 17 (variants: measured, deferred), and the
recommendations that need a confirmed model and a pilot.

Two defects motivate it:

1. **Domains are named after code layers or directories** (`resolvers`,
   `read`, `models`, `broker`, `tasks`, `pub-sub`), not business concepts.
   The intent is to derive the business concepts the code implements.
2. **The EARS actor slot is always "the system"**: it should name the
   component that owns the behavior.

Evidence comes from the remapped outputs under `~/spec-graphs` (pkg, admin,
ai-guide, assignments, coaches; Nov 2–3) and the webapp sources. Book
references (§n.n) are to Markus Voelter, *How to Understand Almost
Anything* (v2, 2024), as distilled in
`agent-orange/kb/rules/distilled/domain_analysis/markusvoelter_htuaa_book_rules.md`.
The book is about designing domain languages forward with experts;
codetospec works backwards from code, so its rules apply as signals and as
process, not as language design.

The framing rule is §7.12: derived properties are computed by analysis,
not encoded in structure. A domain is a derived property of a rule; today it
is read from the structure (directory, package name).

## Findings

**1. Why domains come out as layers.** Nothing asks which business concept
a piece of code serves. `DomainResolver.Resolve`
(`internal/extract/extract.go:277`) uses, in order: an extractor's line-range
`domain` fact (webapp: resolver code takes its schema file's name), the Go
package name unless several directories declare it (`broker/read/` →
`read`, 75 rules), else the first directory under `--src`. So
`content-assignment` (resolvers, from `content_assignment.graphql`) and
`content-assignments` (the `content_assignments/` directory) are one concept
named by two sources — different layers of it, not duplicates (no
near-duplicate pairs between them). `deprecated` comes from
`deprecated.graphql`.

**2. `nature` cannot separate subject matter from technical rules.** pkg's
`datastore` domain marks 48 of 105 rules business; `shared-tasks` 22 of 24.

**3. Business vocabulary exists in the code, in specific places:** federated
GraphQL types the service *owns* (`@key`) as opposed to types it extends;
tables and model structs; enums; events and topics; `models.*` condition
variables (concept attributes).

**4. Evidence for a single Assignment concept comes from schema and models**,
not from duplicates: the federated `Assignment @key` type; the code mapping
each assignment type onto one unified assignment with an assignment-type
enum; the `assignments` table touched by rules of 34 domains.

**5. Variation dimensions in the near-duplicates.** Recomputed over all
pairs with the `internal/consistency` algorithm (reproduces the README's 67
and 66 pairs exactly): the dominant dimension is **content level** (course
/ unit / lesson / domain: 13 of 67 pairs in assignments), not assignment
type. Only 2 of 67 pairs cross domains at 0.8; at 0.6, 46 of 255 do
(course-mastery ↔ mastery 6, content ↔ mastery 3) — weak. Minor dimensions:
draft/scheduled, added/removed, html/text, ela/math (ai-guide).

**6. Time and lifecycle cut across domains:** 232 assignments rules in 30
domains mention due, archived, scheduled, draft, past or deadline (keyword
match, broad).

**7. Security is not all authorization:** 293 of 382 security rules carry
access-control evidence (217 extractor rules); the rest are audit trails
(change events) and data scoping.

**8. Ownership is partly in the schema:** 10 `REFERENCES`, all `NOT NULL`,
from child tables to `assignments` and to `assignment_students`; no
`ON DELETE CASCADE`. A NOT NULL key to a single parent is composition
evidence. The extractor's `table` facts carry only `columns`, no foreign
keys. The rest of ownership lives in delete and archive code paths.

**9. Rule ids are tied to domains.** `MatchRules` only matches a prior rule
of the *same domain* (`internal/graph/stable.go:48`): a rule that changes
domain gets a new id. Consequences: crosscheck and acceptance caches are one
file per rule id (`crosscheck.go:225`, `acceptance/writer.go:155`), so the
LLM judges each moved rule again; reviewed rules are kept only while the run
produces their id; `targets.yaml` (keyed by rule id) and resolutions about
the rule are orphaned; `specdiff` (same matching) reports removed + added,
not renamed.

**10. modelith already holds what a person-owned concept file needs:**
`glossary` (terms, roles), `entities` with relationships
`ownership: owned | referenced` (§7.1), `actions` with an `actor` (a role),
invariants, scenarios, `imports` + `shared: true` (one definition for
shared vocabulary, §3.1, §7.2). One model = one context. No synonym or
counterexample fields: they go in the definition prose. Gaps in codetospec:
`--domain-model` only *compares*, it never assigns rules to domains;
`--write-domain-model` writes one generated model per *current* domain, so
it would carry the layer domains into the models.

**11. Two different actors.** Every rule's subject is "the system". The
**EARS subject** should be a system component; the **role** (teacher,
parent, internal service) is the modelith action `actor` and belongs in EARS
conditions. "coach" (240 coaches rules) vs "teacher" (155) is likely a
hierarchy — coach over teacher and parent — not synonyms; unconfirmed in
code, a question for an expert.

**12. Layer cannot be read from action verbs:** "return" is 35–45 % of first
verbs in every output.

## Recommendations

### A. A person owns the concepts, in modelith (§2.3, §5.1, §3.7, §5.9)

1. Use modelith models as the concept file, one per business context:
   glossary for terms and roles (synonyms, differences, a §3.4 "does not
   cover" in the definition), relationships with ownership (§7.1), `imports`
   for concepts owned elsewhere such as `Classroom` (§7.2).
2. codetospec proposes draft models from code evidence (C), never one per
   current domain. A person confirms; only confirmed concepts reorganize the
   output; proposals are listed in the output README.
3. Close the loop: a confirmed model drives domain assignment (D), not just
   comparison.

### B. Split subject matter from technical before grouping (§1.1, §6.4, §7.17)

4. Classify each rule from evidence, not `nature`. Technical: cited in pkg
   or `shared-*` only, touches no entity or concept, only translates errors
   or status codes, serves performance or reliability. Subject matter:
   touches a concept's entity or attributes, reached from a concept's
   endpoints. Compare with the LLM `nature` (as `position` does); a
   disagreement is a question.
5. Authorization (~290 rules) is a facet of each concept, not a domain
   (§6.4's authorization viewpoint "stated against domain models"); audit /
   change events another facet.
6. Boundary error handling (§7.17) goes to the platform; domain
   preconditions ("reject if invalid") are a facet inside the concept.

### C. Concept candidates from the code (§7.4, §7.13, §3.1, §7.1, §4.3, §7.2)

7. Seeds: owned federated types (`@key`), tables and model structs, enums,
   events, `models.*` condition variables. Extended types (`extend type
   Classroom`) are references to other services' concepts (§7.2). Cruft
   schema files (`deprecated.graphql`) are excluded.
8. Taxonomy (§7.13): an is-kind-of hierarchy, e.g. Assignment ⊃ Content /
   Mastery / Course Mastery / Activity / Writing Coach (finding 4). Content
   level (course > unit > lesson) is a variation dimension, not a concept.
   Coach ⊃ Teacher, Parent is a question for an expert.
9. Terminology checks (§3.1): one concept with several names
   (`content-assignment` / `content-assignments`), one name for several
   concepts.
10. Ownership (§7.1): extend the webapp extractor to emit foreign-key facts
    from migrations (NOT NULL to a single parent ⇒ `owned`); add delete and
    archive paths from code; record as modelith `ownership`.
11. Cross-cutting concerns (§4.3): propose time and lifecycle (232 rules, 30
    domains) as a first-class concept or facet ("Assignment lifecycle"); a
    person decides which.

### D. Each rule gets a concept from evidence (§7.12, §3.3, §3.6, §7.20)

12. Evidence, strongest first: endpoints whose journeys reach the rule
    (owned types only); touched entities and their ownership; enum values and
    `models.*` variables in the condition or cited code; concept terms in
    cited identifiers. A rule reached by several sibling concepts goes to
    their nearest common ancestor.
13. A rule with no concept evidence is technical (B) or cruft (F) — never
    assigned by directory.
14. Layers are detected by how widely their rules spread across concepts,
    not by name. Each rule's layer (persistence / data structures /
    consistency rules / processes, §7.6) is tagged from cited paths, callees
    and entity edges, not verbs; a dependency that goes up a layer or skips
    one is a question.
15. Disagreement is material (§3.3, §3.6): conflicting signals make a
    question with each option, its evidence and what is already settled —
    no majority vote.
16. Declared vs computed (§7.20): once a person has declared the concept of
    a file or rule in the model, a disagreeing assignment is a question.

### E. Granularity and variants (§7.9, §5.10, §4.8, §5.8, §4.4)

17. Rules that differ along one dimension (finding 5) become one
    parameterized entry listing its variants (e.g. course / unit / lesson
    resolution).
18. Split or merge by measured distribution (§5.8): when most rules of two
    sibling concepts differ only along one dimension, propose merging under
    the parent; otherwise keep them apart. Current measurements favor
    keeping assignment types as subconcepts, shared rules lifted to
    Assignment.
19. Invariant behavior stays in the platform (§4.4): rules inherited from
    pkg (`shared-*`, 591 in assignments) are already separate and stay out
    of business domains.

### F. Bounds and cruft (§4.15, §4.10, §1.3)

20. Per service, a person records what is in scope (core rules and their
    data) and out (rendering, emails, import/export, operator commands);
    out-of-scope code gets adapter or platform components.
21. Cruft questions: `deprecated`, one-off commands (`cmd-class-15005-purge`),
    conditions with hard-coded ids or flags → "still needed?", never a
    domain.
22. Some services may be thin (§1.3): the analysis says so instead of
    inventing concepts.

### G. Actors

23. EARS subject = the owning component, set mechanically from the rule's
    concept (business) or platform component (technical, where package
    names are legitimate). No LLM, no remap: the sentence is re-rendered
    from its components.
24. Roles (teacher, parent, service, operator) go in modelith action
    `actor` and the glossary, derived from the rule's own guard, then the
    guards of endpoints whose journeys reach it, then the entry-point kind;
    never in the EARS subject.

### H. Identity before any reorganization (§7.3, §5.2)

25. Decide first, because reorganizing churns ids (finding 9).
    Recommended: `MatchRules` matches across domains when the confirmed
    model declares the move, and the old id is kept — the id is identity,
    its slug a historical name. Caches, reviewed rules, `targets.yaml`,
    resolutions and `specdiff` renames survive. Alternative: a one-time
    migration rewriting ids, caches and person-owned files together (§5.2):
    cleaner, costlier, riskier.

### I. Process (§1.4, §2.2, §5.6, §5.7, §6.3, §6.6)

26. Pilot on one whole service (§5.6): assignments, with one main expert
    (§2.2); review in short cycles after each regrouping (§1.4).
27. Measure on copies of the outputs before changing the pipeline; record
    in a new `docs/verification/` file. Bring a large restructuring to the
    expert with its size stated (§5.7). Check candidate concepts against
    stated target architecture or product direction (§6.3).
28. An LLM may suggest names for clusters without a seed, never decide
    (§6.6). Assignment stays deterministic.

## Next steps (read-only, on copies)

1. Done: subject-matter / technical split (B) on assignments and coaches,
   `scripts/domainsplit.py`, measured in
   `docs/verification/domain-split-2026-11-03.md`. Location decides 96 % of
   it in services that inherit their platform from pkg; the exceptions
   (subject matter in pkg, boundary handling in service code) are the
   information.
2. Drafted: seeds for assignments as a modelith model (lints with no
   error), awaiting a person's corrections of its QUESTION items. Seeds
   found: one SQL aggregate (`assignments` owns its configs and
   `assignment_students`, which owns per-student data, by NOT NULL foreign
   keys); six types under `BaseAssignment`; course mastery a separate
   Datastore aggregate; Datastore models of SQL types vestigial (cruft).
3. Concept assignment (D) on the corrected concepts: coverage, signal
   agreement, questions raised, id churn with and without recommendation 25.
