# Concept regrouping, package map and concept map, 2026-11-10

The README's domain map drew Go packages as if they were business
domains (DOMAIN_PLAN.md: "Domains are named after code layers or
directories"), and its dependency edges were guesses by package name:
an import of `cmd/pull-topic` (package `pull_topic`) found no domain, and
no edge reached any inherited `shared-*` domain in the 25 graphs that
inherit (0 of 41–55 per webapp service, 0 of 2–26 per districts-jobs
module). Decided: regroup first, then draw two honest diagrams.

1. **Regrouping** (TODO "Apply the concept regrouping"): with
   `--domain-model`, each rule concept.Assign placed moves into its
   model's context domain (`concept.Regroup` → `graph.Move`), listed under
   its concept; only confirmed models move rules.
2. **Package map**: the code's package dependencies from the import
   facts, resolved exactly by import path (the Go extractor's new
   `package` facts), shared packages drawn apart, the common prefix
   elided.
3. **Concept map**: the confirmed models' concepts and relationships
   with their rule counts, else the proposal, marked as such.

## Revisions and commands

- codetospec `2410cdd` plus this change; webapp `~/khan/webapp` at
  `c55b04b` (the mapped commit); districts-jobs from a worktree at
  `bc74efd` (the mapped commit; the checkout has moved to `dde732546`).
- Scratch copies of `~/spec-graphs/assignments`, `districts-jobs/roster`
  and `khanxadmin`, re-extracted with this branch's extractors
  (`DRY_RUN=1 scanwebapp.sh` / `scandistricts.sh`, the scratch states'
  `src_path` pointed at the worktree), run with the remap's flags and the
  LLM on a closed port.
- assignments twice: without `--domain-model`, then with its generated
  proposal saved as `assignments.modelith.yaml` and passed as
  `--domain-model` (taken as confirmed, as
  `concept-assignment-2026-11-04.md` did; no person has corrected it).

## Results

Every run came from the caches: 0 tokens, 0 failed chunks (assignments
451, roster 203, khanxadmin 14). The new `package` facts don't enter any
map cache key.

**Regrouping (assignments, proposal as confirmed):**

| | without | with |
|---|---|---|
| domains | 87 | 85 |
| rules | 2307 | 2307 |
| rules moved | — | 786, into `domain.assignments` |
| conflicts / no concept evidence | — | 11 / 178 |

- The 786 rules came from about 35 code domains (`content-assignments`
  78, `content-assignment` 69, `read` 68, `all-assignments` 62, …), under
  14 concepts (`Assignments` 618, `AssignmentStudents` 56, …): the size
  of finding 4 in DOMAIN_PLAN.md, now applied.
- `assignable-entity-id`, `cmd-delete-assignments`, `student-data` lost
  every rule and had no coverage gap: dropped, with the edges to them.
- Every rule file kept its name: no rule id changed (a moved rule keeps
  its first domain's segment, as MatchRules does across runs).
- The context domain lists its rules under concept headings, takes the
  model's description as its summary, and depends on the services its
  rules' domains called (8).

**Package maps:**

| graph | packages | edges | shared drawn | drawn |
|---|---|---|---|---|
| assignments | 39 | 135 | — (webapp runs the Go extractor without `-shared`) | yes |
| khanxadmin | 7 + 11 shared | 28 | 11 | yes |
| roster | 52 + 31 shared | 358 | 31 | source only (over 300 edges) |

- khanxadmin: `khanxadmin/cmd → khanxadmin/cmd/pull-topic` (the edge
  the name matching lost), `khanxadmin/cmd/pull-topic →
  khanxadmin/pkg/cmd` (a package of its own, no longer conflated with
  `khanxadmin/cmd`), and the shared packages each imports
  (`pkg/errors`, …).
- roster's map draws in 97 s outside a run, as the edge budget predicts
  (`policy-views-2026-11-10.md`: 378 edges took 119 s).

**Concept maps:** assignments' proposal, 28 concepts in one frame titled
"assignments (proposal)", the 10 ownership edges from the foreign keys;
with `--domain-model`, the same frame titled after the model and each
concept with its rule count ("Assignments, 618 rules").

## Limits

- No confirmed model exists: the regrouping numbers use the unreviewed
  proposal, whose taxonomy is absent and whose change-event kinds cause
  the 11 conflicts (TODO's pilot with an expert).
- Technical, open and unplaced rules keep their code domains: the output
  mixes context domains and code domains until every business rule has
  a concept.
- A webapp service's package map shows only its own packages: its scan
  doesn't pass the shared tree to the Go extractor.
- Large package graphs (roster) stay as source; an overview per
  directory is not built.
