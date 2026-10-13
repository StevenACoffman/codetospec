# Improvements from neighbouring projects

Survey of `~/Documents/git/{codegrapher, code-graph, gortex, codegraph-go,
zen-go, feelc, go-rule-engine}` (October 2026) for ideas that would make
codetospec, or `extractors/webapp`, more accurate, more deterministic or
cheaper in LLM tokens. Each item gives the source location (checked against
the code, not the READMEs), the concrete application here, and whether it is
a **library** (importable) or an **idea** (port the design).

**Assumption: codetospec is on Go 1.27.** That makes `go-rule-engine`
(`github.com/faustbrian/go-rule-engine/v2`, `go 1.27.0`) importable. It is:
- MIT, tagged v2.0.1, with semantic versioning (`COMPATIBILITY.md`);
- free of dependencies (its `go.mod` has no `require`), so the main module's
  locked dependency set is untouched;
- a public root package.

A scratch prototype built against it with Go 1.27.1 (see #2) confirmed the
API used below.

Everything else is ideas to port. feelc, gortex and codegrapher keep their
code under `internal/`, and zen-go links prebuilt Rust archives through cgo.
Licenses: feelc and gortex are Apache-2.0 (copied code needs NOTICE
attribution); zen-go, go-rule-engine and codegraph-go are MIT.

## Status (2026-10-13)

Each item was measured on the users and districts runs (and, for the
extractor, on admin, ai-guide and content-editing) before being built.

| # | Outcome | Evidence |
|---|---|---|
| — | **Done: code-aware grounding fold** (was "grounding tiers", filed as minor) | 11 of districts' 33 ungrounded rules were *extractor* rules: a call split over lines read `f( ktx, x)` against `f(ktx, x)`. Now 22; users unchanged; no regression |
| 3 | **Done: grounding repair in map** (never worse) | ~1% of rules ungrounded → about 40 extra exchanges per service; a fix is kept only if valid, keeps every rule, grounds more |
| 9 | **Done: scattered-domains report** (gonum Louvain + `topo` split) | on the pre-fix runs it flags both known problems: districts `main` (41 files, 5 communities) and `schema` (35 files, 4); users `resolvers` (7), `schema`, `main` |
| — | **Done: extractor bug found by #2 step 1's prototype** | a block ending in a successful return was a guard because an auth error sat in a nested branch; `errorName` now skips nested branches (districts −1 false rule, nothing else changed) |
| 2 (step 0) | **Rejected by measurement** | 5 candidates (users 4, districts 1), 0 real contradictions: complementary branches or paraphrases with one negation word |
| 1, 2 (step 1) | **Deferred** | prototype with go-rule-engine over 84 multi-guard methods: 0 dead, 0 shadowed guards once variables are keyed by their bound call; nothing to report without LLM conditions |
| 5 | Deferred | needs LLM conditions; extractor examples would be tautological |
| 1 (LLM part) | Deferred | a map prompt change re-maps every chunk (~3.6M tokens per service), and LLM variables would not share the extractor's vocabulary |
| 4, 8 | Deferred | 0 aliased `acl`/`permissions` imports in any service; ~4 cross-package access calls; cost = type-checked fixture + slower extractor |
| 6 | **Rejected by measurement** | rules per 1,000 lines are flat with distance from resolvers (users 46 at hop 0 vs 49 unreached; districts 33 vs 24): skipping far chunks drops rules in proportion |
| 7 | **Rejected by measurement** | exact Type-2 clones: users 6.1% of map lines (mostly mocks, testutil, a data list, now excluded by `scanwebapp.sh`), districts 1.5% |
| 10–13 | Deferred | no consumer yet |

Go is now 1.27 (go.mod), and gonum v0.17.0 is a dependency of the main
module. Go 1.27's vet also required wrapping tree-sitter's `QueryError` by
value (`internal/sitter/sitter.go`).

## Priority order

| # | Improvement | Source | Kind | Gain |
|---|---|---|---|---|
| 1 | Structured `condition` per rule | go-rule-engine | **library** | accuracy, consistency |
| 2 | Contradiction, overlap and gap detection | codetospec `negations`, go-rule-engine, feelc `verify` | library + idea | consistency |
| 3 | Grounding failures fed into the map correction loop | feelc `ingest` | idea | accuracy |
| 4 | Type-aware webapp extractor (`go/packages` + `go/types`) | none (gortex shows the walk) | library (x/tools) | accuracy |
| 5 | Executable examples checked against the condition | feelc `check`, go-rule-engine `Plan.Evaluate` | library | acceptance criteria |
| 6 | Relevance ranking to skip low-value chunks | codegraph-go RWR, code-graph ranker | idea | tokens |
| 7 | Type-2 clone detection before map | gortex `clones` | idea (port) | tokens, consistency |
| 8 | Reaching definitions for guard bindings | gortex `cfg`, or `go/ssa` after #4 | idea | citation fidelity |
| 9 | Deterministic communities as a domain check | gonum `graph/community` (Louvain) + `graph/topo` | **library** (gonum) | domain quality |
| 10 | Explicit rule anchors in comments | codegrapher | idea | ground truth |
| 11 | Tests that exercise a cited function as evidence | codegrapher | idea | crosscheck |
| 12 | Stable diagnostic codes | feelc `diag` | idea | tooling |
| 13 | Decision-table export (JDM / DMN) | zen-go, feelc | idea | interoperability |

## 1. Structured `condition` per rule — use go-rule-engine

**Source.**
- go-rule-engine's grammar (`model.go`, `operator.go:42-70`,
  `serialization.go`, `docs/json-ast.md`): predicates
  `True/False/All/Any/Not/Exists/Compare`; operands are `Variable(path)` or
  `Literal(value)`; operators `equal, not_equal, less_than, less_or_equal,
  greater_than, greater_or_equal, in, not_in, contains, starts_with,
  ends_with, matches`.
- `ParseJSON` (`serialization.go:140`) is strict and returns
  `[]Diagnostic{RuleID, Code, …}`: a bad node yields
  `invalid_json: predicate kind is invalid`, verified in the prototype.
- `MarshalCanonical`/`CanonicalHash` (`serialization.go:72-135`) give a
  stable encoding and a sha256.

**Application.** Add an optional `condition:` (the canonical JSON) to rule
frontmatter next to the EARS text.
- **webapp extractor.** Build it mechanically from `guard.cond`, which
  `renderer.cond` already walks (`&&`, `||`, `!`, nil tests). Each check
  becomes a boolean variable compared to a literal:
  `acl.IsLoggedIn(…)` → `caller.loggedIn equal false`. A predicate call such
  as `permissions.UserCanViewDistrict(ktx, id)` becomes the variable
  `permissions.UserCanViewDistrict`. The grammar has no call operands, so the
  arguments stay in the EARS text and the citation. `err != nil` on a bound
  call becomes `<call>.fails equal true`. Verbatim, unknown conditions stay
  opaque variables: honest and still comparable.
- **LLM rules.** Have the map step propose a condition. `ParseJSON` rejects
  malformed ones with a diagnostic, which feeds the correction loop (#3).
  Grounding then checks the condition's literals against the cited lines,
  as it does for the requirement text.
- **Canonical hash.** Two rules with the same condition hash equal even
  when their wording differs, which Jaccard misses.

**Limits.** The predicate tree can't be walked through the API: node types
and `Plan.requiredPaths` are unexported, and so is the JSON schema behind
`MarshalCanonical`. codetospec builds every condition itself, so it knows the
variables. For LLM-proposed JSON, a small codetospec decoder of the documented
grammar lists them. Per-set limits (`limits.go:49-60`: 1,000 rules, AST depth
64) are far above any domain here; the largest districts domain has 51
mechanical rules.

## 2. Contradiction, overlap and gap detection

Three steps of increasing cost:

**Step 0: text-level contradiction candidates (no new dependency).**
`internal/consistency` deliberately never pairs two rules with different
negation counts (`mayDuplicate`/`negations`, `consistency.go:93-110`).
"Shall reject X" and "shall not reject X" are therefore reported as neither
duplicates nor contradictions today. Report pairs above the Jaccard threshold
*with* different negation counts as **contradiction candidates**, in their
own section of the output README. A few lines of code.

**Step 1: boolean conditions, enumerated by go-rule-engine.** For each
operation (or domain), compile its rules into one `RuleSet{Strategy:
CollectAll}`. Then evaluate every true/false assignment of the variables
they read, which is 2ⁿ inputs for n variables; authorization guards use a
handful. Each row's `MatchedRules` gives:
- overlap: two rules match the same input
- containment: every input that rule A matches also matches rule B
- dead rules: a rule matches no input.

Verified in a scratch prototype: two rejection rules for one operation
(`not svcAdmin AND not currentUser`, `not loggedIn`) over 3 variables gave
8 rows. Exactly one row matched both rules (the overlap), and three rows
matched neither (callers who are allowed).

What it means depends on the rules' outcomes. With only rejection rules, an
unmatched input means "allowed", not a gap. A **gap** or a **contradiction**
needs rules with *different* outcomes over the same variables: one
operation's reject rules plus an LLM rule saying the same callers are
served. Contradictions are thus the realistic win between mechanical and
LLM rules. Cap n (say 16 variables, 65,536 evaluations) and report "not
verifiable" above it, as feelc does, never a partial proof.

**Step 2: numeric conditions, ported from feelc.**
`internal/verify/verify.go:85-250` collects the cut points of each numeric
input, walks the grid between them (`gridBudget = 500_000`, line 29) and
classifies the results: `gap`, `conflict`, `dead-rule`,
`unreachable-default`, `not-verifiable`, `subsumed` (lines 38-44). Port only
the cut-point generation and feed the points to `Plan.Evaluate`, so the
matcher stays go-rule-engine's. Worth it only once LLM rules carry numeric
conditions (rate limits, thresholds).

Jaccard stays as the fallback for rules without a condition.

## 3. Grounding inside the map correction loop

**Source.** feelc `internal/service/ingest.go:96-222`. Each round, the LLM
drafts, the engine compiles and verifies, and blocking findings go back as a
fixed-format message. There are `defaultIngestRounds = 3` rounds, hard
limit 5 (lines 21-22), and the code decides when to stop.

**Current state.** The mapper already retries (`validateMapReply`,
`internal/mapper/mapper.go:402`), but only on structure: JSON, citations,
line ranges. Grounding runs once, after the build
(`cmd/codetospec/main.go:505`), and only marks a rule `ungrounded`.

**Application.** `grounding.Check(requirement, citations, opts)`
(`internal/grounding/grounding.go:147`) is pure, so `validateMapReply` can
call it for each mapped rule. A `Citation` needs the whole file's lines
(`grounding.go:42-47`). The mapper knows `chunk.Path` and must read the file
once; `Options.Constants` comes from the facts, as in `main.go`. An
ungrounded value becomes a correction: "`429` does not appear in lines
40-52; cite the line that holds it, or drop the value." It costs tokens only
on failures and fixes defects instead of reporting them. With #1, a
`ParseJSON` diagnostic on a proposed condition goes into the same message.

## 4. Type-aware webapp extractor

**Finding.** None of the four code-graph tools is type-aware for Go: every
call and `implements` edge is a name heuristic (codegraph-go
`internal/indexer/dispatch/implements.go`, which tags edges
`Provenance="heuristic"`; codegrapher `resolve/resolve.go`). gortex's "SSA +
VTA" (`internal/semantic/goanalysis/provider.go:36`) is only a comment: no
file imports `go/ssa` or `go/callgraph`. What gortex does show is the
pattern: `go/packages` loading, then a walk over `TypesInfo.Uses` mapped to
the enclosing function (`resolveGoUse`, :1968).

**Application.** `extractors/webapp` is plain `go/ast`, so today:
- `permissions.X` is matched by package *name*, and an aliased import is
  missed (stated in `accessPredicates`' contract)
- a guard reached through a helper in another package is invisible
- `ktx.RequestIs…()` is matched by method-name prefix.

Load the service with `golang.org/x/tools/go/packages` (`NeedTypes |
NeedTypesInfo | NeedSyntax`), as `extractors/go/main.go` and `routes.go`
already do. Then:
- resolve each call to its `*types.Func`, so aliases work and look-alike
  names don't match
- read the `bool` result from the signature
- follow helpers across packages.

Add `go/callgraph/vta` only if interface dispatch matters for guards. Cost:
the extractor type-checks the service, which is slower and needs the
module's dependencies downloaded.

## 5. Executable examples

**Source.** feelc `internal/check/check.go`: a claim
`{desc, decision, input, expect}` (line 20), evaluated by the deterministic
VM as `supported` or `contradicted` (lines 31-32). go-rule-engine's
`Plan.Evaluate(ctx, Context)` (`plan.go:127`) is the matching library call:
the result has `MatchedRules` and, when evaluation fails, an `Indeterminate`
decision.

**Application.** Where a rule has a condition (#1), write its acceptance
criteria as `{given: {var: value…}, expect: matches | does-not-match}`.
Check them with `Evaluate`: a contradicted example flags a wrong requirement
or a wrong condition, with no LLM. The reducer already writes acceptance
criteria as free text, which grounding deliberately ignores, so this gives
them a checkable form. For mechanical rules, the extractor can produce the
examples itself: one row matching the condition, one not.

## 6. Rank chunks to skip low-value ones

**Source.**
- codegraph-go `internal/query/rwr.go`: random walk with restart, fixed 25
  iterations with no early exit (line 64), α = 0.25, scores rounded to 1e-9
  (line 71), ties broken by ID. Byte-deterministic, about 60 lines, no
  dependencies.
- code-graph `api/mcp/tools/structural.py:41-56`: a hybrid score with
  weights name 2.0, path 1.5, BM25 1.5 and centrality 0.5, minus a 1.0
  penalty for test/vendor/migration paths.
- gortex `internal/analysis/processes.go:269` (`scoreEntryPoint`).

**Application.** Seed the walk with route, resolver and entity facts, then
rank chunks per domain over the import and call graph. Chunks far from any
entry point (formatters, getters, glue) are skipped or batched, which cuts
map tokens directly; the users and districts runs are dominated by such
plumbing. Make it opt-in (`--rank-min`), and list skipped chunks in the
output README so coverage stays honest.

## 7. Type-2 clone detection before map

**Source.** gortex `internal/clones/`. `tokenize.go` collapses identifiers to
placeholders; `minhash.go` builds `NumHashes = 64` (line 37), seeded by a
fixed xorshift (line 78); `lsh.go` bands find candidate pairs in roughly
linear time.

**Application.** webapp resolvers repeat near-identical bodies (the same
guard and the same loader for different fields). Map one representative per
clone class, then re-cite the rule on each copy, after checking mechanically
that the cited lines match token for token. The repeated rules would also be
grouped deliberately, rather than caught afterwards as Jaccard
near-duplicates.

## 8. Reaching definitions for guard bindings

**Source.** gortex `internal/cfg/`: `builder.go`, `defuse.go`, and
`reaching.go` (bitset reaching definitions, `ChainsFor`).

**Application.** The webapp scanner approximates this: `forget`
(`extractors/webapp/authz.go`) drops any local a nested statement reassigns.
Reaching definitions would:
- cite exactly the definitions that reach the `if`
- keep a binding reassigned only on a path that returns (dropped today)
- reject guards reached by two definitions.

Do it with #4, not by porting gortex: a `go/ssa` function gives def-use for
free, since each SSA value has one definition.

## 9. Deterministic communities as a domain check — add gonum

**Library.** `gonum.org/v1/gonum` v0.17.0 (BSD-3-Clause, pure Go), package
`graph/community`.

gonum has **no Leiden**: `graph/community` provides Louvain (`Modularize`,
`louvain_common.go:90`; directed, undirected and multiplex variants), plus
`Q` for modularity, `KCliqueCommunities` and `Profile` for sweeping the
resolution. Leiden's main gain over Louvain is that every community is
connected. gonum covers that too: split each Louvain community into its
connected components with `graph/topo.ConnectedComponents`. That is
Leiden's guarantee without its refinement phase, using only gonum code.

**Verified in a scratch prototype** (Go 1.27.1, gonum v0.17.0):
`community.Modularize` on a weighted undirected graph of two triangles
joined by a weak edge separated the triangles. Two runs with the same
`rand.NewPCG(1, 2)` seed gave identical communities, and
`topo.ConnectedComponents` confirmed each community is connected.
`go list -deps` shows that the build pulls only gonum's own packages
(graph, mat, blas, lapack, floats) besides the standard library; the
`gonum.org/v1/plot` and `x/tools` requirements in gonum's `go.mod` are
pruned and not built.

**Determinism**, the pattern codegraph-go already uses with gonum
(`internal/query/community.go:30-211`):
- insert nodes in sorted order with stable IDs
- pass a fixed `rand.NewPCG` seed to `Modularize` (their line 138)
- sum the edges between each pair of files into one weight
- relabel communities by their smallest member path.

codegraph-go relies on this for byte-identical output, which is codetospec's
own rule (two warm-cache runs must be identical).

**Graph.** Nodes are files (or symbols); edges come from the existing
`import` facts and SCIP `symbol` references, weighted as gortex does
(`internal/analysis/communities.go:439-448`): calls 3.0, membership 2.0,
references 1.5, imports 0.5. gortex's weights are the one thing worth
taking from it; its hand-written Leiden is not needed.

**Why not a full Leiden.** Leiden adds two things over Louvain: connected
communities, which the `topo` split restores, and somewhat better partitions
from its refinement phase. The second doesn't change whether a mis-assigned
domain gets flagged. gortex's Leiden (`internal/analysis/leiden.go`) is
written from scratch: 5 tests, 6 commits, tied to its `graph.Store`.
gonum's Louvain has about 44 tests across its variants and years of
releases. A future gonum release may change partitions even with the same
seed, so pin the version, and keep communities in the README report rather
than in the generated spec.

**Measure first.** Before wiring it in, run it once over the districts and
users import/symbol graphs. Keep the item only if it flags the two problems
found by hand: the catch-all `schema.graphql` and the Go `main` packages.

**Application.** Keep the domain strategy, which is predictable and
explainable, and use communities as a check reported in the output README:
- directory domains that split across communities
- communities that span several domains.

That's how the districts `schema.graphql` catch-all and the Go `main`
packages would have been flagged without measuring by hand. A later option
is to split oversized reduce batches along community boundaries instead of
by count.

**Cost.** One new module in the main `go.mod`, a widely used scientific
library; it requires Go 1.24 (`go.mod`), well below codetospec's 1.27.

## 10. Explicit rule anchors in comments

**Source.** codegrapher `docs/semantic-index.md:40-50`. A comment
`// modelspec: implements chinook.Invoice.total` just above a declaration
creates an edge with provenance `explicit_annotation`. Names are never
inferred.

**Application.** Let authors pin a rule: `// codetospec: rule <slug>` plus a
one-line EARS requirement. It becomes a mechanical rule with an exact
citation: no LLM call, and ground truth to measure the map phase against.
It's another fact kind in the tree-sitter extract.

## 11. Tests as evidence

**Source.** codegrapher `internal/cli/context.go:1991`
(`testCallersOf(store, nodeID, maxHops)`): incoming calls from `_test.go`
functions, 1 hop by default.

**Application.** For each rule, list the test functions that call its cited
function. Show their names and assertion literals to the crosscheck, and
render them in the rule node. Note that `config.DefaultExclude`
(`internal/config/config.go:73`) does not exclude `_test.go`, so tests are
*mapped by the LLM* by default; only `scanwebapp.sh` excludes them. Indexing
tests as evidence in the extract step instead would also make excluding them
from the map the sensible default, a token saving outside webapp too.

## 12. Stable diagnostic codes

**Source.** feelc `internal/diag/diag.go`: codes `DSL001…DSL010`, never
renumbered, each with a message and a suggestion. go-rule-engine's `Code`
values (`errors.go:10-38`: `invalid_json`, `type_mismatch`, `cycle`, …)
cover its own engine only.

**Application.** Give verify, grounding, drift and crosscheck findings stable
codes (`CTS-G001 ungrounded number`…) and a suggestion. CI can then filter
by code, and #3 sends the suggestion verbatim as the correction.

## 13. Decision-table export

**Source.** zen-go JDM (`test-data/table.json`): a JSON graph of `nodes`
(`inputNode`, `decisionTableNode` with hit policy, inputs, outputs and rules
whose cells are unary expressions like `"> 10"`) and edges. feelc
`internal/dmnxml/export.go` writes DMN XML.

**Application.** Once rules have conditions (#1), write each operation's
authorization rules as one decision table: inputs are the checks, the output
is allow or reject with an error. Owners can open and simulate it in the
GoRules editor. Write the JSON directly.

## Already present, or not worth taking

- **Content-hashed LLM caches.** Both code-graph surveys recommend keying map
  output on chunk hash plus extractor and prompt version. codetospec already
  does this: the cache file is named after the chunk-content hash, and
  `promptKey` (`internal/mapper/mapper.go:316`) folds in the model,
  language, prompt templates and the injected context sections. That
  includes the mechanical rules, so an extractor change re-maps exactly the
  affected chunks.
- **Skipping crosscheck for mechanical rules / provenance tiers.** gortex
  `internal/contracts/bodyfacts.go:16-19` (`text_matched`, `ast_inferred`,
  `ast_resolved`, `lsp_resolved`). codetospec already marks extractor rules
  `extracted_by` + `crosscheck: mechanical`. A finer tier is worth adding
  only if a review UI needs it.
- **go-rule-engine's derivation-cycle check** (`compiler.go:249`,
  `findDerivationCycle`). It runs inside `Compile` and reports `cycle`, but
  only for rules that derive facts. codetospec rules don't chain ("shall set
  X" feeding "IF X"), so there's nothing to check yet.
- **gortex GraphQL support** (`internal/contracts/graphql.go`) is
  regex-based, with no gqlgen or genqlient. The gqlparser-based webapp
  extractor is already better. One transferable idea is in `wrapper.go`:
  propagate up the callers of a client wrapper to recover the literal each
  caller passes. That's useful if genqlient calls start going through local
  helpers.
- **Grounding tiers** (gortex `internal/analysis/grounding.go:83`: exact,
  whitespace-normalized, token overlap). The whitespace tier turned out not
  to be minor: see Status (code-aware fold, done). Token overlap stays out:
  it would pass wrong values.
- **Incremental `--since <ref>`** (gortex `internal/analysis/diffmap.go`).
  Redundant: content-aware caches already re-map only changed chunks.
- **cgo-free tree-sitter via `odvcencio/gotreesitter`.** codegrapher depends
  on it (v0.20.2), but its ADR (`docs/adr/003-parser-strategy.md:17`)
  measured 330 s and 7.7 GB on one Go file, and moved Go parsing to
  `go/parser`. If the cgo-free route is revisited, keep the wazero approach,
  or use `go/parser` for Go files.
- **zen-go as a dependency.** cgo with prebuilt `libzen_ffi.a` archives of
  about 40 MB per platform (`cgo.go`, `deps/`), and `zen_engine.h` exposes
  evaluation only; the static analysis its README advertises isn't there.
  Its JDM format is still the export target for #13.
- **feelc's FEEL evaluator.** feelc vendors a fork of
  `github.com/pbinitiative/feel` v1.0.6 (MIT) via `replace` (`go.mod:8,19`)
  to fix a parser loop. FEEL is richer than needed for boolean guards, and
  go-rule-engine covers #1, #2 and #5.
- **code-graph.** Python/FalkorDB with no Go analyzer. Only its ranker (#6)
  and its no-LLM token-savings benchmark
  (`benchmarks/mcp_context_benchmark.py`, a template for measuring #6 and #7)
  are relevant.
