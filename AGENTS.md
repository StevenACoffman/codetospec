# codetospec — agent guide

Go binary that reads a repository and produces a markdown graph of business
rules (EARS), cited line by line and verified. For the what/why, see
`README.md`. Here: how to work in the repo.

## Commands

```sh
go build -o bin/codetospec ./cmd/codetospec   # ALWAYS rebuild before a real run
make test                                      # go vet + go test, all modules
make check                                     # lint + test + race + tidy + vuln (= CI)
make corpus                                    # real graphs vs baselines, no LLM
make run-fixture                               # real run on testdata/fixture (needs LLM_BASE_URL)
```

- Lint: the repo's `.golangci.yml` (copy of `~/Documents/git/.golangci.yml`),
  golangci-lint ≥ v2.14 (2.13 cannot read go1.27.2 export data). Never
  relax it; a `//nolint:<linter> // <reason>` only when the code is correct
  by design (nolintlint requires the reason).
- `make corpus` (`internal/corpus`, tag `corpus`) replays grounding, verify
  and drift on the graphs of `internal/corpus/testdata/corpus.json` (local
  outputs under `~/spec-graphs`) and fails if a count gets worse; an entry
  that is missing or whose source has left the recorded commit is skipped.
  `CODETOSPEC_CORPUS_UPDATE=1` re-records the baselines (after review).
- **Rebuild `bin/codetospec` after any code change before running it** —
  a stale binary silently produces a wrong run (it has happened).
- CGO required (tree-sitter): `CGO_ENABLED=1`, C toolchain required.
- Measuring the LLM crosscheck on planted faults (opt-in, calls the
  model): `CODETOSPEC_EVAL=1 go test ./internal/crosscheck -run Eval -v`
  (with `LLM_BASE_URL`, `LLM_MODEL`). Rerun before adopting a new prompt or
  model: `CODETOSPEC_EVAL_REPEAT=3`, `CODETOSPEC_EVAL_RECORD=f` then
  `CODETOSPEC_EVAL_BASELINE=f` to compare. Adoption criterion: balanced
  accuracy (mean of the rate of faults caught and the rate of controls
  accepted; "refute everything" = 0.5) no lower, false alarms no more
  numerous, no more cases that flip; a failed call counts as an error.
  `TestScoreEval` pins this computation. `TestPlantedFaults` (grounding) is
  the mechanical equivalent, always on, blind spots listed explicitly.
- `go test ./...` runs without network or PHP. Tests tagged `phplocal` for
  the PHP extractor (skipped if `php` is missing).

## Architecture

Pipeline: `extract → chunk → map → reduce → crosscheck(+repair) → quality → build → digest → test reach → verify → render`.
- `internal/sitter` — tree-sitter, one grammar + one `.scm` per language.
  The chunker groups small adjacent definitions (same file, namespace and
  domain) up to 150 lines (`pack`): the fixed cost of a map call (~570
  tokens) dominates small functions.
  Adding a language = grammar + query file, zero change to the core.
- `internal/extract` — Fact model, merge, external extractor protocol.
  Pitfall: `Merge` deduplicates by ID and a `module` fact has the ID
  `module.<name>` — only one remains per name. Any "per file" computation
  (e.g. `AmbiguousNamespaces`, the Go `main`s of each `cmd/`) therefore
  reads the tree-sitter `fileInfo`, not the merged facts; test it through
  the real path (`sitter.Parse` → `UniversalFacts` → `Merge`), not with
  hand-built facts.
- `internal/{mapper,reducer,crosscheck}` — LLM phases, each mechanically
  validates its output (2 corrections max then a traced failure). The map
  prefixes each line of code with its absolute number (`numberedContent`):
  otherwise the model *counts* lines and drifts (off-by-N citations on big
  chunks). The crosscheck (`--crosscheck`) refutes each rule; with
  `--repair` (+ SCIP index), a rejected rule re-cites the exact span of a
  precise symbol, chosen by label (`S1`…, `symbolCatalog`): the model never
  writes a range, so the citation is a real symbol body (mechanical
  grounding), then it is
  re-reviewed blind (`review` receives neither the first verdict nor its
  reason). It is adopted only if that review says `supported`; otherwise the
  rule keeps its verdict (`repair_review` traces it).
- `internal/{graph,verify,render}` — deterministic assembly, checks, writing.
- `internal/drift` — digest of the code cited by each rule (frontmatter
  `digest:`); `drift` command = re-hashes and reports stale rules
  (deterministic). With `call` facts, `stamp` also writes the direct callees
  of the cited code (`callees:` + `callees_digest`, a single hop: the
  closure would cover the whole module); a modified callee makes the rule
  `affected`, reported but never a failure. `source_hashes` (hash of the
  text alone of each span, without path or numbers): a different digest
  whose every text is found elsewhere in its file (nearest window) makes the
  rule `moved`, never a failure. `drift.Stamp` sets these four fields (the
  core does not know their keys). `drift --format json` lists each rule.
  Standalone: `drift` does not read `.codetospec/`.
- `docs/verification/` — measurements on real code behind a decision
  (revisions, commands, results, limits); a new measurement = a new file,
  never a rewrite.
- `internal/testreach` — rules reached by a test, no LLM: the `test` facts
  (Go extractor: what each `TestXxx`/`FuzzXxx` or suite method `TestXxx()`
  uses — webapp has almost only those —, itself or through the helpers of
  its `_test.go` files) then the `call` facts, at most `MaxDepth` = 3
  definitions. Frontmatter `tests` (0 included), `test_depth`, `tested_by`;
  with no `test` fact at all, nothing (unknown ≠ untested), nor for a rule
  that cites only another root. Pitfall: the extractor loads the tests
  (`Tests: true`) but all other facts read the packages without tests
  (`loadedModule.pkgs`), otherwise the test variants would duplicate
  symbols and calls; an object of a test variant is not the package's: the
  declarations are indexed by position.
  Calls through an interface (`extractors/go/dispatch.go`): `x.M()` on an
  interface *of the module* gives one `call`/`test` fact per
  implementation, marked `dispatch: interface` (`Fact.ViaDispatch`). Each
  consumer chooses: `tests` ignores them (no existing count moves),
  `tests_via_interface` counts the extra tests; `callers` and drift's
  callees ignore them (the implementations include test doubles outside
  `_test.go`). Interfaces of other modules (`error`…) are not followed:
  `err.Error()` would reach everything.
  `-cover profile` (Go extractor): `executed` facts per file (executed /
  not executed lines); `executed: k/n` per rule, observed, next to `tests`,
  derived. A file newer than the profile is dropped; a rule that cites a
  file without a profile does not have the field.
- `internal/quality` — `--quality`: classifies each technical rule by ISO
  25010 characteristic (list from spec-artifacts-iso 0.3.0, Quire),
  frontmatter `quality_attribute`. Batches of 40 labeled R1…Rn (the model
  writes only labels and values from the list, otherwise correction),
  single cache `.codetospec/quality.json` by hash (prompt key + title +
  requirement): a renamed rule keeps its answer. A failed batch leaves its
  rules without an attribute (asked again on the next run), never a run
  failure. Outside map/reduce: no cache invalidated. The passes after the
  build (crosscheck, quality, stamps, test reach, reviewed rules) are in
  `cmd/codetospec/judge.go`.
- `graph/enums.go` — `enum` nodes (`nodes/enums/`) from the `enum`,
  `enum_value`, `transition` facts (Go extractor, `enums.go`,
  `transitions.go`): values in code order (sorted by citation: the facts
  arrive sorted by id), table of transitions, `transitions:` in the
  frontmatter. A transition = assignment `x = B` under a guard `x == A`
  (if, `&&`, case); an assignment without a guard is not one. Generated
  files (`ast.IsGenerated`) ignored: their enumerations mirror an external
  schema. Few transitions on real code (4 in districts): state changes
  mostly in SQL.
- `internal/quire` — `export --out --to` command: rules → FRs in Quire
  format (spec-artifacts-iso 0.3.0, pinned in the package doc: the only
  place that knows the format). Pure (`Export` returns the files); the CLI
  refuses a non-empty `--to` (one-time bootstrap, the team then edits in
  quoin). Without criteria (reduce `merge`), the EARS requirement becomes
  the only criterion (`codetospec_criteria: requirement`); a rule refuted by
  the crosscheck: dropped. Verified against the downloaded frontmatter
  schemas (jsonschema) on roster and districts; no local `quire validate`
  (no published binary).
- `internal/kiro` — `export --format kiro`: one Kiro spec per domain
  (`.kiro/specs/<domain>/requirements.md`, `design.md`), format pinned in
  the package doc (Kiro docs + example spec from kirodotdev/Kiro
  @bfe7ff3); one requirement per rule, its EARS sentence in Kiro capitals
  (`ears.InCapitals`) = criterion N.1, its acceptance criteria = the
  property that "Validates: Requirements N.1"; trace (id, verdict, code)
  in an HTML comment; no user story (the code does not tell one), no
  tasks.md. Pure; actors described by `render.LoadActors` (graph.json).
  Verified: each exported criterion of districts and assignments reads
  back into its rule's components
  (`docs/verification/kiro-export-2026-10-27.md`).
- Frontmatter keys: declared once in `frontmatterExtras` (render); reading
  (`extraOf`) and verify's round-trip check (`render.FrontmatterKeys`)
  derive from it.
- `internal/ears` — EARS components, generic syntax `[Trigger]
  [Precondition] Actor Action [Object]` (`ears.Components`: `where`
  features and `while` preconditions 0..n, `when` and `if` 0..1 each —
  both = two triggers, complex requirement —, `system` the actor with its
  article (article lowercased, name as is: "THE Issue_Manager" → "the
  Issue_Manager"), empty = "the system", limited to the `actors` of
  codetospec.yaml (`config.Actors`: a description, or `{description,
  aliases}`; `ALLOWED_ACTORS` = the names only, outside promptKey like the
  entities; an alias becomes its name without correction
  (`mapper.Actors.Named`), including in the cache after a rename; a cached
  actor no longer listed becomes "the system" (`Actors.Listed`); the output
  README lists the actors no rule names), `shall` 1..n `{text, action}`,
  one item per action ("log an error and return X" = two; measured before:
  ~26 % of rules wrote several as one,
  `docs/verification/ears-lint-go-2026-10-28.md`): the action = first
  words of the text, required from the model, kept next to the sentence
  (`ears_actions`) because the boundary cannot be read back from a
  sentence; `graph.RuleComponents` reads it all back, an action that an
  edit no longer puts first becomes unknown): the map and the reduce write
  them instead of the sentence and the kind; `Render` writes the sentence
  (order WHERE, WHILE, WHEN, IF … THEN, several parts joined by AND/ET in
  capitals, responses joined by "; "), `KindOf` the kind (`complex` from
  two keywords; `Patterns` the simple patterns combined, frontmatter
  `ears_patterns`, derived from the sentence for every rule in
  `graph.withEARS`), `Normalized` repairs the model's deviations without
  an LLM (keyword in the value, final THEN, "the system shall").
  `mapper.Rule`/`reducer.Rule`: `Requirement` and `EarsKind` are derived
  (`json:"-"`, `WithText` at decode and at cache read): a rule's JSON is
  exactly what the model reads and writes. `Parse` reads a sentence (any
  language) back into components: `graph.json` carries them for each rule
  (absent if the sentence does not read, 1.3 % today,
  `docs/verification/ears-components-2026-10-27.md`). Contract pinned by
  `FuzzRenderParse`: `Parse(Render(c)) == c` and `Check` accepts the
  sentence. The keywords live only in `wordings()`, also read by `Check`.
  `Parse` reads the conditions in any order (SI … ALORS last),
  "QUAND t AND p" (the precondition goes into `while`; a lowercase "and"
  stays in the trigger: undecidable), and any actor "the <name>"
  (`actorSpan`, shared with `Check`). `splitClauses` is the only one that
  knows what separates two clauses (comma, surrounding whitespace ignored,
  keyword): `Validate` (`checkClause`) consults it too, otherwise
  `Parse(Render(c))` would drift. `earslint_test.go` replays the cases of
  labeth/ears-lint-go we agree on.
  `ears.Check` (FR and EN, accents and case ignored): a single
  "shall/doit" whose subject is an actor with an article, opening of the
  kind (QUAND, TANT QUE, LÀ OÙ, SI … ALORS after another clause;
  ubiquitous = opens on the actor); `complex` = sentence read by `Parse`
  with at least two patterns (measured: no regression on 19,081 rules,
  `docs/verification/ears-generic-syntax-2026-10-27.md`).
  `Build` writes `ears_issue` on the faulty rule (counted in the output
  README and the corpus); a faulty extractor rule is rejected (the
  extractor is deterministic), an inherited rule kept and reported.
  Pitfall: tightening `ears.Check` silently drops extractor rules (the
  webapp templates write "shall … only when …");
  `TestExtractorRuleFactsPass` validates the webapp goldens, and
  `make corpus` recounts the `ears_issue`s (baselines to re-record).
  No check of conditions after "shall" nor of uncited guards: measured,
  ~25 % precision and ~1–2 % of rules
  (`docs/verification/ears-clauses-2026-10-27.md`).
- `graph.MatchRules` — stable ids: before the build, `render.LoadPrior`
  reads the previous output; an LLM rule that takes one up again (same
  domain, and same requirement, or overlapping citations with Jaccard
  ≥ 0.6 or same id) keeps its id. Extractor rules take their id first; an
  id from the previous output is never given to another rule on the next
  run (`assignRuleIDs`). The crosscheck cache (key = id) benefits.
  Same matching for the renames of `internal/specdiff` (`diff --old --new`
  command: added, removed, renamed, `reworded`, `code`, `moved`,
  `verdict`; always exits 0).
- Hand-reviewed rules (`render.LoadPrior`, `render.KeepReviewed`):
  `graph.json` records the hash of each node file; a rule file modified
  since (or marked `status: reviewed`) belongs to a person and is rewritten
  as is, `status: reviewed`, as long as the run produces its id. It keeps
  its own digest: `drift` reports it when its code changes. To regenerate
  it: delete the file. A reviewed rule whose citation no longer resolves
  stops the run before any LLM call; another modified node type is
  overwritten (warning).
- `internal/questions` — findings reviewed by hand: unanchored rule,
  `ears_issue`, refuted, stale inherited verdict, coverage gap → questions
  in `questions.json`, id = hash (kind, subject, stable key, digest of the
  cited code): not the wording (the `@…` location of a value moves), but
  the code (a change reopens the question). `resolutions.yaml` (the
  person's, never written): `verified` or `out-of-scope` + reason; read by
  `render.LoadPrior`, an invalid entry stops the run before any LLM call. A
  resolved question is no longer listed in the README; a resolution
  without a question is counted. The render does not know the ids
  (`Ledger.ResolvedRule`, `ResolvedGap`).
- `graph.AnnotateCoverage` — `coverage: complete | partial` per domain,
  with `coverage_gaps` (generated file, failed map chunk, failed reduce),
  computed by `coverageGaps` in `pipeline.go`; "Code without rules" section
  of the output README. A path that is excluded or has no grammar has no
  domain: it never makes a domain partial.
- `internal/atomicfs` — the only writes of the output: `WriteFile` (temp +
  rename) and `ReplaceDir` (`nodes/`, `models/` built alongside then
  swapped); an interrupted render leaves the previous output intact.
- `internal/locale` — all fixed output strings, FR and EN side by side
  (`tr(fr, en)`), including the EARS templates of the prompts. Writers take
  `locale.For(--lang)`; parsers (`graph.RuleRequirement`,
  `graph.RuleCriteria`) accept the labels of every language.
- `internal/grounding` — mechanical check (no LLM): every number / quoted
  string of the EARS requirement must appear in the cited lines.
  `grounded | ungrounded | unchecked` in the frontmatter; "unchecked" is
  never a success. Only the requirement is checked (the acceptance
  criteria contain test examples absent from the code). Numbers are read
  with the decimal separator of `--lang` (`locale.Text.DecimalSeparator`):
  a single reading, never both — accepting "1,000 = 1" would wrongly pass
  every English rule whose code contains a `1`. The map also applies it
  along the way (`mapper.Ungrounded`, injected by `main` to avoid the
  grounding → graph → mapper cycle): a reply with ungrounded rules gets a
  correction exchange, adopted only if it is better (never worse;
  `mapper/ground.go`). That correction text is not part of `promptKey`: the
  map cache stays valid; `--reground` replays the exchange on cached
  outputs, once each (`regrounded` field of the output). A citation that is
  only too narrow is widened without an LLM (`grounding.Annotate`,
  `widening`): value present on a single other line of the file, ≤ 30
  lines away, not a lone digit; traced in `grounding_widened`.
- `internal/graph/handlers.go` — mechanical rule → endpoint link
  (`exposed_by`) when a citation overlaps the handler's code
  (`handler_path` + `handler_lines` of a `route` fact): holds on a warm
  cache, where the map is never asked again (the allowed endpoint lists
  are not in `promptKey`).
- `extract.Roots` — the only reader of cited files: `--src`, plus the
  `--ref-root name=dir` trees a citation names (`Ref.Root`: the shared
  code, `pkg` for webapp and districts-jobs). A cited root without
  `--ref-root` stops the run before any LLM call (`Roots.CheckRoots`).
  Grounding, drift, verify go through it; `Ref.File()` (`@name/path`)
  serves as key and display. An empty `Root` changes nothing (digests,
  goldens, caches).
  An extractor rule can be **inherited** from another spec
  (`inherited_from` + its verdict): `graph.Reviewable` keeps it out of the
  crosscheck, the render never writes `extracted_by` for it.
- `internal/communities` — "scattered" domains: communities of the import
  graph (gonum's Louvain + connected components, fixed seed), section of
  the output README. gonum is pinned: a version bump can change the
  communities.
- `internal/inherit` — inherits from a spec of shared code (`--inherit
  name=graph`, requires `--ref-root name=dir`): each LLM rule with a single
  citation falling in reached code (`reach` facts, emitted by an extractor,
  e.g. `extractors/go -shared pkg=…`) becomes an inherited rule of the
  `shared-<domain>` domain. Its verdict holds only for the judged code: if
  the shared code no longer has the shared spec's `digest`, the rule is
  `unchecked` and keeps the old verdict in `stale_verdict`. The core reads
  its own spec format; the extractor only says what is reached.
  `scandistricts.sh` applies it to districts-jobs, `scanwebapp.sh` to
  webapp (`reach` facts from the webapp extractor).
- `internal/callers` — after chunking, a chunk without a `domain` fact
  takes the domain its callers **in the same directory** agree on (`call`
  facts, emitted by the Go extractor), callers decided first (Kahn order,
  ready nodes sorted: deterministic); a call cycle keeps its domains. A
  caller from another directory is ignored: otherwise library code takes
  the `main` domain of its only caller, and the shared `models` that of
  `servicetest` (the real callers are in other modules).
- `internal/consistency` — detection of near-duplicate rules (Jaccard on
  the requirements), listed in the output README.
- `graph.SharedEntities` — entities touched by rules of several domains
  (`touches` links from the reduce), "Shared entities" section of the
  output README: shared kernel or misplaced boundary.
- `internal/domainmodel` — comparison with modelith models written by
  humans (`--domain-model`, read only, no LLM), via
  `github.com/StevenACoffman/indomain` (importable fork of modelith,
  admitted into the main module). Lint first: an error stops the run
  before any LLM call. Entities matched by name (normalized, regular plural
  accepted, or `domain_model_entities`), invariants ↔ rules that touch the
  entity by Jaccard (threshold 0.3, EARS template words removed); section
  of the output README. Outside `promptKey`: no cache invalidated.
  `--write-domain-model`: `domainmodel.Enhance` (pure, returns the files)
  writes under `<out>/models/` the enriched human models (yaml.v3 nodes:
  comments and order kept; definitions E → C → A, the placeholder prefix
  makes it recognized on the next round) and one
  `generated/<domain>.modelith.yaml` per domain for the entities without a
  model, plus `provenance.json`. A written model read back as human does
  not change (round-trip test).
- `cmd/codetospec/main.go` — the full sequence.

## Extractors = SEPARATE Go modules

`extractors/php/` (composer), `extractors/go/` (own `go.mod`, depends on
`golang.org/x/tools`), `extractors/scip/` (depends on `github.com/scip-code/scip`
+ protobuf) and `extractors/webapp/` (Khan webapp/gqlgen, depends on
`vektah/gqlparser/v2` + yaml) **are not** built by `go build ./...` from the root.
Test/build them from their folder. Reason: the main module has a locked
dependency set (yaml, tree-sitter, charm) — the extractors live outside
that set. The SCIP converter and the Go extractor emit `symbol` facts with
`precise: true` (same IDs, they merge) that the map injects as citation
anchors; for Go, SCIP is therefore optional (`SCIP=0` by default in the
scripts). The Go extractor also emits the `constant` facts (webapp no
longer emits them) and `call`.

`constant` facts (grounding: `400` ↔ `http.StatusBadRequest`), `doc`
(`SCHEMA_DOCS` section of the map), `dependency` (`service` nodes +
`depends_on`; `as` = `service-admin` | `user`, the authority of the call)
and `route.handler_path` complete the contract. The reduce has a
`--reduce-mode merge` mode (`reducer.ModeMerge`): only the candidates in a
cluster (Jaccard ≥ 0.6 or overlapping citations, `internal/textsim`) go
to the LLM.

`rule` and `domain` facts (contract: constants of `internal/extract`): an
extractor can provide deterministic rules (validated by
`graph.ValidateRuleFacts`, rendered `extracted_by` + `crosscheck: mechanical`,
listed to the map as `MECHANICAL_RULES`) and domains by line range
(`extract.DomainHints`, taking priority over the domain strategy). No
repository-specific code in the core: all webapp knowledge lives in
`extractors/webapp`.

## Cache & resume (`<out>/.codetospec/`)

- `map/<chunkID>.json` — name = hash of the chunk content; reused if its `prompt_key` matches. The domain of a cached output is taken from the current chunk (changing the domain strategy regroups without re-mapping).
- `reduce/<domain>.json` — reused if the hash of the candidates (`candidates_hash`) matches.
- `crosscheck/<ruleID>.json` — key = hash (prompt key + rule + cited lines).
- `quality.json` — a single file, answer by hash (prompt key + title +
  requirement); keeps only the run's rules.

All four keys include `llm.PromptKey`: model, language (if the prompt depends on it) and **text of the
prompt templates** (+ `--repair` for the crosscheck). Editing a prompt or
changing the model therefore invalidates the cache on its own — no version
constant to bump. A cache without a key (earlier version) is considered
stale.

Resume = existence of these files. To replay a phase, **purge its
cache** (`rm -rf .codetospec/reduce`) then rerun; the remaining upstream
phases are reused. `state.json` accumulates tokens across runs and records
the source (absolute path): `state.Open` refuses an `--out` that belongs to
another source (`state.ErrOtherSource`), before any write — `start` opens
the state before writing `facts.json`.

The three phases are *content-aware*: map (key = hash of the chunk
content), reduce (key = hash of the domain's candidates, stored in the
`.json`) and crosscheck (key = hash of rule+lines). After a code edit, the
re-run automatically redoes what changed — no more manual purge of the
reduce. The reduce also runs in a worker pool (`--workers`), the domains
being independent (`depends_on` is computed at build).

## Conventions

- Deterministic output: stable sorts everywhere, two warm-cache runs =
  byte-for-byte identical. Never introduce non-deterministic order.
- Everything in **English**: code, comments, identifiers and user-facing communication.
- No global logger: `main` builds a `*slog.Logger` (`newLogger`) and passes
  it explicitly (`runPipeline`, `Log` fields of `Mapper`, `Reducer`,
  `Checker`, argument of `extract.RunExtractor`, `ui.Plain`). Never
  `slog.SetDefault` nor `slog.Info`; tests pass a logger on `t.Output()`.
- Render golden tests: `UPDATE_GOLDEN=1 go test ./internal/render` to regenerate (no global flag; same for `internal/domainmodel` and `extractors/webapp`).
- README kept up to date with each shipped feature (concise).

## Notes

The reduce automatically splits domains of more than `--reduce-batch`
candidates (default 30) into batches merged deterministically; a batch
that still truncates is split in two again and retried (fast halving,
without wasted corrections) down to a floor. A big domain no longer loses
its rules. The cross-batch merge deduplicates by exact requirement and
renumbers colliding slugs (no fine semantic dedup across batches: accepted
limit). Beware: a domain of several hundred candidates (e.g. GraphQL
resolvers) stays slow — often plumbing to exclude upstream.
