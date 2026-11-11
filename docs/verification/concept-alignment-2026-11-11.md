# Concepts against the code, 2026-11-11

Three ideas from a review of deterministic architecture measures
(reflexion models, recovery against a ground truth, asking the smallest
question), applied where anarch-lint can't reach: business rules placed
in a person's concepts, in any language. With `--domain-model`:

1. **Pairs** (`alignment.Measure`): each unordered pair of concepts
   converges (declared and shown), diverges (shown, not declared: a
   `concept_divergence` question), is absent (declared, not shown), or
   unobservable (declared, one concept with neither placed rules nor a
   table). Shown means a foreign key between the concepts' tables, or a
   direct call from code one concept's rules cite into code another's
   cite.
2. **Scattering and tangling**: the directories each concept's rules
   cite, the concepts each directory serves, rule counts beside them.
3. **Recovery**: the adjusted Rand index of the import graph's
   communities against each file's majority concept.
4. **Conflict sets** (`concept.ConflictSets`): the concept conflicts as
   the fewest questions that settle them, in greedy order.

## Revisions and commands

- codetospec `5e074cc` (branch `graph-import-dirs`) plus this change;
  webapp `~/khan/webapp` at `c55b04b` (the mapped commit).
- The scratch copy of `~/spec-graphs/assignments` from
  `concept-regrouping-2026-11-10.md`, run with the same flags, its
  generated proposal as `--domain-model` (taken as confirmed, as before;
  no person has corrected it), the LLM on a closed port.

## Results

Every run came from the caches: 0 tokens, 0 of 451 chunks failed.

| measure | assignments |
|---|---|
| declared relations (the proposal's) | 10, all `owned`, from NOT NULL foreign keys |
| pairs converging | 10 |
| pairs diverging | 16 (`concept_divergence` questions) |
| pairs absent / unobservable | 0 / 0 |
| concepts with placed rules | 14 (`Assignments` 618 rules in 24 directories) |
| busiest directory | `resolvers`: 290 placed rules, 11 concepts |
| recovery | adjusted Rand index 0.00 over 162 files (10 concepts, 5 communities) |
| concept conflicts | 11, in 6 concept sets, settled by 4 questions (4, 4, 2, 1) |

- Every divergence is shown by calls, none by a foreign key: the 10
  foreign keys between concept tables are all NOT NULL, the proposal
  declares each as an ownership, so they can only converge here. On this proposal the 10 convergences are circular
  by construction; with a model a person wrote they would not be.
- The divergences name the pairs a person would check first: the
  change-event kinds called from `Assignments` code (17 calls into
  `CourseMasteryAssignmentChangeEvent`), `SubjectMasteryAssignment`
  archiving calling `MasteryAssignmentConfig` code. They are the same
  concepts the 11 conflicts name.
- The index is about 0: the communities don't recover the concepts. The
  proposal puts 618 of 786 placed rules in one concept, so most files
  vote `Assignments`; the import graph splits the same files by package.
  That is consistent with "packages are not business domains", and not a
  verdict on either.
- The four conflict questions: `CourseMasteryAssignmentChangeEvent` with
  `SubjectMasteryAssignment` (4 conflicts); `Assignments` with both
  change-event kinds (4, among them the three of `Assignments` with
  `UnitMasteryAssignmentChangeEvent`); `AssignmentStudents`,
  `Assignments` and `SubjectMasteryAssignment` (2); the course change
  event with `MasteryAssignmentConfig` (1).

**Determinism.** The first two runs differed in the "Scattered domains"
section and the index: the same files and imports (dumped from both
runs and compared) gave 9 or 10 communities. In one process, `Of` on the
dumped input differed in 27 to 31 calls of 40. gonum's Louvain walks
nodes and neighbours in the order the graph gives, and a `simple` graph
gives its maps' order, so ties broke differently each time. This
predates the change (the binary of `5e074cc` printed one of those
variants three times in a row, by chance). Fixed with a graph that
iterates by id (`communities.sortedGraph`) and communities numbered by
their smallest file: 0 calls of 40 differ, and three runs of assignments
are byte-identical (README, questions, graph). `TestOfDeterministic`
fails without the fix. The fix changes the "Scattered domains" lists of
earlier outputs: on assignments, 4 domains became 5 (`content-assignment`
added, `platform-assignment` in 2 communities instead of 3).

## Limits

- No confirmed model exists: every figure uses the unreviewed proposal
  (TODO's pilot with an expert).
- The placement of rules in concepts rests on their `touches` edges,
  which the reduce writes, and on condition variables: the comparison is
  deterministic given the placement, and each rule's `concept_evidence`
  says what placed it.
- A call counts for every concept whose rules cite its caller and its
  callee; code that rules of several concepts cite relates all of them.
- Imports are not evidence: most packages serve several concepts.
- The index compares partitions of different grain (10 concepts, 5
  communities); a file citing concepts equally is left out.
- An imported entity named `alias.Entity` is qualified with the alias, as
  the ownership and the concept map already do.
