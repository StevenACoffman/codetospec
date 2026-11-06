# Resolver journeys: handler_lines on GraphQL route facts

Question: once the webapp extractor's route facts carry the resolver
method's span (`handler_lines`), do GraphQL endpoints gain journeys, and
what else moves (`exposed_by`, nature evidence, caches)?

## Revisions and commands

- codetospec working tree on `ccc0566`, with FIX.md R1-R3 and
  `policy_inventory_plan.md` Part 1; webapp `c55b04b0090`.
- Before: `~/spec-graphs/{districts,users}` from the 2026-11-05 remap.
- After: copies of those outputs, rerun warm with the remap's flags:
  `OUT_DIR=<copy> PKG_GRAPH=~/spec-graphs/pkg ./scanwebapp.sh <svc>
  --quality --crosscheck --write-domain-model`.

## Results

| | districts before | after | users before | after |
|---|---:|---:|---:|---:|
| endpoints | 209 | 209 | 149 | 149 |
| endpoints with a journey | 12 | 209 | 4 | 149 |
| `exposed_by` edges (graph.json) | 780 | 780 | 712 | 712 |
| rules | 3,924 | 3,896 | 3,071 | 3,071 |
| LLM tokens of the rerun | — | 0 | — | 2,118 |

- Every GraphQL endpoint now has a journey; before, only `/pubsub` push
  handlers (Go extractor) had one. districts' journeys: 6 of length 1,
  127 of length 10 or more (the cap is depth 3, not length).
- `exposed_by` doesn't move: every rule citing resolver code already had
  its edge, extractor rules through their `route` attribute and LLM rules
  through the map's endpoint lists. So nature evidence, `nature_mismatch`
  questions and concept placement don't move either: the plan expected
  them to, and they don't.
- districts' 28 fewer rules are not Part 1's. They're inherited pkg rules
  (`shared-dstest` 26, `shared-datastore` 2) that districts reached only
  from `testutil/`: with FIX.md R2 (`-exclude-dirs testutil`) the webapp
  extractor no longer walks it, so 50 `reach` facts go (29 into
  `pkg/gcloud/datastore/dstest`) and those test-utility rules leave
  districts' spec, as intended. The `technical` count falls by exactly 28.
- users' 2,118 tokens are one crosscheck call. The crosscheck key folds in
  a rule's two nearest tests (AGENTS.md), and FIX.md R3 gave some rules
  new tests through package-level variables, so one key changed. Map,
  reduce, quality and acceptance stayed cached.

## Limits

- Two services. Journeys of 10 or more rules are common on districts; how
  useful a long journey is to a reader isn't measured.
- The corpus isn't rerun here: it measures `~/spec-graphs`, which this
  measurement leaves untouched, and counts neither edges nor journeys.
