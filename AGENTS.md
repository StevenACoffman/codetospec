# codetospec — agent guide

Go binary that reads a repository and produces a markdown graph of business
rules (EARS), cited line by line and verified. For the what/why, see
`README.md`. Here: how to work in the repo.

## Commands

```sh
go build -o bin/codetospec ./cmd/codetospec   # ALWAYS rebuild before a real run
make build                                     # also bin/codetospec-d2 (renderers/d2), the diagram renderer
make test                                      # go vet + go test, all modules
make check                                     # lint + test + race + tidy + vuln (= CI)
make corpus                                    # real graphs vs baselines, no LLM
make run-fixture                               # real run on testdata/fixture (needs LLM_BASE_URL)
scripts/remap.sh                               # remap every ~/spec-graphs output + its measurements (DRY_RUN=1 first)
```

- Lint: the repo's `.golangci.yml` (copy of `~/Documents/git/.golangci.yml`),
  golangci-lint v2.15.0, pinned in `mise.toml` and CI (2.13 cannot read
  Go 1.27.3 export data). Never
  relax it; a `//nolint:<linter> // <reason>` only when the code is correct
  by design (nolintlint requires the reason).
- `make corpus` (`internal/corpus`, tag `corpus`) replays grounding, verify
  and drift on the graphs of `internal/corpus/testdata/corpus.json` (local
  outputs under `~/spec-graphs`) and fails if a count gets worse; an entry
  that is missing or whose source has left the recorded commit is skipped.
  `CODETOSPEC_CORPUS_UPDATE=1` re-records the baselines (after review).
  A full remap goes through `scripts/remap.sh`: snapshot, pkg twice
  (run-to-run variance, `scripts/remapcheck.py`), services,
  districts-jobs, then the baselines re-recorded and their diff shown;
  it refuses a dirty tree and a checkout that moves during the run.
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
- Measuring `--quality` on Dalpiaz's relabeled PROMISE set (opt-in,
  calls the model and downloads the GPL-3.0 data at run time, never
  committed): `CODETOSPEC_EVAL=1 go test ./internal/quality -run
  EvalPromise -v`, `CODETOSPEC_EVAL_RECORD=f` to keep the score.
  `TestScoreQuality` pins the scoring (PROMISE O counts as portability
  or compatibility).
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
- `internal/llm` — the one OpenAI-compatible client, shared by every phase
  and worker (`pipeline.go`). Each failure carries a toerr `errcode`
  status (`classify`, `throttle.go`, pure): resource exhausted (429, or a
  5xx naming a wait) closes a `gate` every call waits on, for the wait the
  reply names (`Retry-After`, Gemini's `RetryInfo.retryDelay`) or a
  minute, capped at 5, up to 10 pauses per call; unavailable or deadline
  exceeded (network error, timeout, other 5xx) retries that call alone
  after jittered backoffs; anything else fails. A worker retrying on its
  own kept a throttle going for the others. Pauses and retries log at
  WARN (`NewClient` takes the logger).
- `internal/{mapper,reducer,crosscheck}` — LLM phases, each mechanically
  validates its output (2 corrections max then a traced failure). The map
  prefixes each line of code with its absolute number (`numberedContent`):
  otherwise the model *counts* lines and drifts (off-by-N citations on big
  chunks). Pitfall: every field added to a map rule's JSON (EARS
  components, condition) made the model write fewer technical rules;
  the prompt names observable technical behaviour as a rule to hold
  them (`docs/verification/map-technical-rules-2026-11-04.md`). Measure
  candidates by nature before and after any map-prompt change. The crosscheck (`--crosscheck`) refutes each rule, reading beside its
  cited code the two nearest tests reaching it (`testreach.NearestTests`,
  `Checker.TestsFor`; folded into the cache key; the prompt forbids
  crediting what only a test does,
  `docs/verification/crosscheck-tests-evidence-2026-11-01.md`); with
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
  its `_test.go` files, plus the package-level variables it reads:
  `extractors/go/vars.go`, so a test of a registry table such as admin's
  role definitions reaches the rule citing it; constants and blank
  variables don't count; `docs/verification/testreach-package-vars-2026-11-06.md`)
  then the `call` facts (a package-level variable holding a function,
  such as webapp's `cache.Cache` wrappers, is a caller and a callee,
  its calls marked `callee_var`, a hop the policy inventory's reach
  doesn't count:
  `docs/verification/calls-through-vars-2026-11-06.md`), at most `MaxDepth` = 3
  definitions. Frontmatter `tests` (0 included), `test_depth`, `tested_by`;
  with no `test` fact at all, nothing (unknown ≠ untested), nor for a rule
  that cites only another root. Pitfall: the extractor loads the tests
  (`Tests: true`) but all other facts read the packages without tests
  (`loadedModule.pkgs`), otherwise the test variants would duplicate
  symbols and calls; an object of a test variant is not the package's: the
  declarations are indexed by position. `config.DefaultExclude` leaves
  `*_test.go` out of the map: the Go tests are evidence (read by the Go
  extractor itself, not by the walk), never behavior to map; other
  languages' tests are still mapped.
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
- `internal/quality` — `--quality`: classifies each rule but the
  presentation ones by ISO 25010 characteristic (a business rule can also
  serve a quality: authorization is `security`; list from
  spec-artifacts-iso 0.3.0, Quire),
  frontmatter `quality_attribute`. Batches of 40 labeled R1…Rn (the model
  writes only labels and values from the list, otherwise correction),
  single cache `.codetospec/quality.json` by hash (prompt key + title +
  requirement): a renamed rule keeps its answer. A failed batch leaves its
  rules without an attribute (asked again on the next run), never a run
  failure. Outside map/reduce: no cache invalidated. The passes after the
  build (crosscheck, quality, stamps, test reach, reviewed rules) are in
  `cmd/codetospec/judge.go`.
- `internal/condition` — a rule's structured condition (IMPROVEMENTS #1):
  a compact JSON notation (`all`/`any`/`not`/`exists`/`var`+`op`+`value`
  or `var2`; contract in `extract.AttrCondition`) that codetospec compiles
  into a `github.com/faustbrian/go-rule-engine/v2` plan; no engine type
  crosses the API. `Parse` (an error names the JSON path), `Hash`
  (the engine's canonical hash: equal whatever the spacing or key order),
  `Canonical` (frontmatter `condition`), `Variables`, `Literals`
  (grounding text), `Renamed`, `Disjuncts` (the alternatives over its
  boolean checks, negations pushed to the leaves, capped at 64: what
  path coverage reads), `Evaluate` (an `Outcome`, never an error:
  a missing variable is `Indeterminate`). `matches` is left out (a regex
  is not groundable). Every number is a float (the engine refuses int vs
  float, and a model writes 0 and 0.0 alike): 3 and 3.0 hash equal. `FuzzParse` pins `Parse(Canonical(c))`. Producers:
  the webapp extractor (`extractors/webapp/condition.go`, access and
  rate-limit guards outside loops; `TestExtractorRuleFactsPass` parses
  its goldens' conditions, so a notation drift fails there), the map
  (prompt rule + `VOCABULARY`; a bad or ungrounded condition joins the
  one repair exchange, `reground`, then is dropped, never the rule;
  unknown variables move under `local.`), the reduce
  (`settleConditions`: the merged candidates' shared condition wins over
  the model's; equal hashes also cluster in merge mode; batch merge keeps
  a condition only when both copies agree), inherited rules (carried).
  At build, `grounding.Annotate` drops a condition whose values the cited
  lines lack (`condition_dropped`) and records a disagreement with the
  EARS conditions (`condition_disagreement`, values marked cited or not;
  0 and "" are implicit in prose; a value an extractor carries in
  `check_args`, such as a check's scope matcher or a rate limit's numbers,
  counts as stated: `docs/verification/check-args-disagreement-2026-11-06.md`;
  so does a value in a variable's name or the value of a constant a
  variable names; only the text side is checked, and re-annotation
  replaces a stale disagreement)
  → `condition_mismatch` question.
- `internal/vocabulary` — the condition variables offered to each map
  chunk, no LLM: extractor rule-condition variables (scoped to their
  file), then schema facts (`table.column`, enums with values, struct
  fields `pkg.Type.Field` from the Go extractor's `struct` facts), then
  `--domain-model` attributes and enums; a term is offered when its last
  part is a word of the chunk (case, `_`, camelCase ignored), a struct
  field only when its struct's name or that name's last part is one too
  (else every `Status` everywhere), at most `MaxTerms`. Measured: coined
  variables 97 % → 59 % on districts-jobs
  (`docs/verification/condition-vocabulary-go-structs-2026-11-02.md`). `Coined` moves other variables under `local.`. Injected as
  `Mapper.VocabularyFor`, outside `promptKey` like the entities.
- `internal/acceptance` — `--acceptance`: per LLM rule not refuted, one
  call (cited code via `extract.Roots` + the two nearest tests) writes the
  observable (`response|state|event|log`, check, `test|code`), up to 3
  non-claims and, with a condition, up to 4 examples (IMPROVEMENTS #5).
  Pure `Validate` (examples name only the condition's variables), `Assess`
  (observable grounded against what it was read from; examples evaluated
  by `condition.Evaluate`), `Annotate` (body sections parsed back by
  `graph.RuleObservable`/`RuleNonClaims`; frontmatter `observable`,
  `observable_source`, `observable_grounding`, `examples: k/n`,
  `examples_contradicted` → `example_contradicted` question). Runs in
  `judge` after the stamps (an ungrounded condition is gone by then).
  With `--crosscheck` (`Writer.Review`), a blind review of each
  observable (`review.go`: rule, observable, code, tests; nothing of the
  writing conversation), cached in the rule's file under its own hash
  and kept through runs without it: `observable_crosscheck`, and when
  unsupported `observable_refuted` (the reason) → `observable_refuted`
  question; `acceptance.ExportedObservable` is what Quire and Kiro take
  (none when refuted).
  Exports: Quire one more criterion (`codetospec_observable`) and
  `codetospec_non_claims`; Kiro the property's criteria and the trace
  comment (requirements.md takes EARS only).
- `graph/enums.go` — `enum` nodes (`nodes/enums/`) from the `enum`,
  `enum_value`, `transition` facts (Go extractor, `enums.go`,
  `transitions.go`): values in code order (sorted by citation: the facts
  arrive sorted by id), table of transitions, `transitions:` in the
  frontmatter. A transition = assignment `x = B` under a guard `x == A`
  (if, `&&`, case); an assignment without a guard is not one. Generated
  files (`ast.IsGenerated`) ignored: their enumerations mirror an external
  schema. Few transitions on real code (4 in districts): state changes
  mostly in SQL.
- `internal/quire` — `export --out --to` command: rules → FRs, and
  technical rules serving a quality (`quality.IsNFR`) → NFRs, in Quire
  format (spec-artifacts-iso 0.3.0, pinned in the package doc: the only
  place that knows the format). An NFR's threshold is the target a person
  set (`internal/nfrtarget`, `codetospec_threshold: set`, mapped by kind),
  else the run's `threshold` frontmatter (`quality.AnnotateThresholds`:
  the numbers of a grounded requirement, `grounding.StatedNumbers`, read
  with `--lang`'s decimal separator; with no number stated, the numeric
  constants the cited lines name, `grounding.Quantities` minus enum
  values), else unknown, and the `nfr_target` question id
  (`questions.KindNFRTarget`) only when the requirement states a
  quantity (`quantity: stated`). NFR shape verified with the downloaded
  `nfr-frontmatter.schema.json` and the skeleton's section rules
  (`docs/verification/quire-nfr-export-2026-11-01.md`). Pure (`Export` returns the files); the CLI
  refuses a non-empty `--to` (one-time bootstrap, the team then edits in
  quoin). Without criteria (reduce `merge`), the EARS requirement becomes
  the only criterion (`codetospec_criteria: requirement`); a rule refuted by
  the crosscheck: dropped. Verified against the downloaded frontmatter
  schemas (jsonschema) on roster and districts; no local `quire validate`
  (no published binary).
- `internal/policy` — the policy inventory (`policies` command,
  `policy_inventory_plan.md` Part 2): pure `Build(facts, rules, registry)`
  over the merged facts and the rule nodes' `checks`/`check_args`/
  `operation`; a check's inputs are the `reads`, `uses_value` and `loads`
  facts of its definition and of what it calls (direct `call` facts, by
  span overlap, `MaxDepth` 3); kinds by structure only (a call's `means`,
  `value_kind` facts the extractor emits per declared type), never by
  words or webapp names: the extractor states what each recorded check
  call means (`check_args`: `var` the condition variable it became,
  `fails`, `means`, `scope`, `subject_from`; webapp's table is
  `extractors/webapp/meanings.go`).
  A registry (`ParseRegistry`, a person's JSON or YAML, never written,
  `schema/registry.schema.json`) declares fields, loads and methods
  (`Type.Method()`, joined to the Go extractor's `calls_method` facts),
  and is the glossary: each entry's `phrase` (per language) and
  `concept`.
  Definitions (`define.go`, `inv.Define(checkers, registry, text)`): each
  `Check.Definition` carries the webapp extractor's allowing paths
  (`access_check` `paths`, or `not_analyzable` + reason), the pin, its
  `Statement` (`statement.go`, `locale.Policy`), leaf grounding
  (grounded / ungrounded / unknown, from the inputs and the paths), and
  `path_coverage`: the pin's DNF against each alternative of each path's
  condition (`condition.Disjuncts`), both ways; a path is covered when
  every alternative matches a part (one the pin lacks is a way the code
  allows that the pin doesn't state). In a path, a scope matcher is an
  operand, a call's failure whose decision has its own variable is its
  error, a call nothing states is data; negations and scopes aren't
  compared; a call of a check nothing states inlines that check's own
  paths (`pathReader`, `MaxDepth`, cycles cut); a call within the package
  names its check unqualified (`pathReader.name` qualifies it); an item
  filter's pin never states the call's own bool (a separate decision).
  A checkers entry's `bugs` (`checker_comment_mismatch`, `dead_branch`)
  become `Definition.Questions`, ids from `questions.ID` over the kind,
  the check, the detail and the checker's digest
  (`docs/verification/policy-work-2026-11-08.md`). `inv.Compare(first, second)` (`equivalent.go`): truth
  table over canonical leaves, a counterexample. `Inventory.Access`
  (`access.go`): per concept, each guard stated from its own condition
  (the negated deny, De Morgan to the leaves), or undecided with the
  reasons. Everything a state: booleans; the JSON matches
  `schema/inventory.schema.json` (tested). Schemas are embedded and
  compiled together (`compiled`), so one may `$ref` another.
- `internal/cerbos`, `internal/cedarpolicy` — `export --format cerbos |
  cedar-policy` (`policy_inventory_plan.md` Part 3). Both render
  `policy.BuildModel`: each extractor guard whose condition's variables
  map, through the one call that became each (`CheckCall.Var`), to the
  pinned leaves (`policy.Leaf*`, scopes `policy.Scope*`, operands joined
  from `check_args` and capability facts) becomes a deny. Never
  over-permit: an operation is left out whole when a guard doesn't
  translate, a rule of it names no check (`guard names no check`), its
  resolver reaches a recognized check its guards don't call
  (`checksBelow`, call facts, `MaxDepth`; a check the resolver answers,
  the webapp extractor's `answers`, excepted: returned as its value, its
  error unable to reach the caller), or its checks are about a local or
  about more than two users (`subjectOf`: `resource.subject`, and
  `subject2` for a second user, whose checks are `Leaf.Second`, their
  variables suffixed `@subject2` in the leaf, `Defined` and the condition
  so the oracle tells them apart; a user-scope grant on the second is
  skipped). `Operation.Principals`: the principal kinds some passing
  assignment can come from (`kindsOf`), set when not all: Cerbos
  `allow-<roles>`, the Cedar policy language `when { principal is … }`. An
  operation whose rules all delegate (`delegates_to_operation`) to an
  exported operation is its alias. `export --checkers` (`ParseCheckers`,
  a person's YAML, never written) pins what checks the extractor doesn't
  know mean: one leaf, or `any`/`all`/`not` of leaves (parts named
  `<check>#n`, `Deny.Defined` lets the oracle decide the checker's
  variable from them), `subject_arg` for a user check (on a compound
  checker, passed to its parts) or a relation to an argument's user
  (`R.attr.subjectRelations`; an argument the webapp extractor bound to
  the request user is `from: caller`, `policy.FromCaller`, which the
  subject annotation passes as the principal itself),
  `capability_arg` for a part's capability,
  an `attribute` of the caller, resource or request, the `locale`,
  `explicit` and `content` (`content_kind` or a literal
  `content_kind_arg`) scopes, `cases` keyed on literal arguments
  (`ArgCase`, `Checker.For` the one resolver: a call matching no case is
  skipped; `"@caller"`, `policy.CallerArg`, matches an argument bound to
  the caller whatever its text), `approx`, `item: <Kind>` for a visibility
  filter, with an optional `call` (its call-level meaning, where a guard
  tests the filter itself; `Checker.atCall` is the one substitution)
  (`Model.Items`: a `view` policy per kind, where self and coach are
  about the item's user, `SubjectItem`), `unpinnable: <reason>` (the
  skip reason), `digest` (`policy.StalePins`: `export --src` warns about
  a pin whose checker changed; `policies --src` prints each check's).
  `--checkers` repeats; files merge (`MergeCheckers`, a name in two is
  an error). JSON or YAML, `schema/checkers.schema.json` (an unknown key
  is an error; `TestCheckersSchemaEnums` pins the enums to the Go
  vocabulary). `--join <graph>=<checkers>` (`join.go`): an unpinned
  check calling one other-service operation (`dependency` facts) whose
  guards answer one pinned check takes that pin; anything else reported.
  Formats pinned in each package doc, with the loader contract; the
  subject binding is an annotation (Cerbos `subject:<action>`, the Cedar
  policy language `@subject`). A guard whose every variable is a rate
  limit's (`CheckCall.Rate`) is an `Operation.Obligations` entry, not a
  deny: exported (decided 2026-11-08), annotated Cerbos
  `obligation:<action>`, the Cedar policy language `@obligation`
  (`Obligation.Text`), which both loader contracts make the PEP apply. Verified against the oracle, not the
  rendering: `Operation.Cases()` decides each truth assignment with
  `condition.Evaluate` on the rule's condition; Cerbos writes them as its
  test suite (`go test -tags cerboslocal` runs `cerbos compile`,
  `CERBOS_BIN`), cedarpolicy's test runs `cedar.Authorize` on them.
  Dependencies approved for these: cedar-go (pinned v1.9.0, `x/exp`
  validator) and jsonschema/v6 (Cerbos's vendored schemas in testdata).
  Name the Cedar policy language in full: CEDAR is Khan's analytics
  pipeline. `cedarpolicy.Decider` (`decide.go`) loads a persona
  (`policy.Personas`: the fixed principal kinds, a phantom, a site
  admin, each role granting a checked capability at each grant scope
  `policy.Grant*`, each checked capability held explicitly) as the loader
  contract says, each request fact no persona fixes (`FactsOf`: self,
  coach, a relation or attribute, task, Okta; a user-scope grant's NWEA
  district, a country or locale grant's match) both ways, and asks
  `cedar.Authorize` on the exported text: allowed, denied, or which facts
  it depends on (undecided past 10 facts). `TestDecideAgreesWithCases`
  checks every loaded request against `Operation.Cases`, the checks'
  truths asked of cedar-go one leaf at a time; on the six remapped
  graphs, 0 disagreements in ~99k requests
  (`docs/verification/policy-views-2026-11-10.md`).
- `internal/d2` — every diagram, in D2 (the one place that knows the
  language): callers build a `d2.Diagram` (nodes by class: step,
  decision, terminal, focal, group (a container; `Columns` makes it a
  grid), store, external; edges), `Source` writes it in a `Palette`
  (`Light`, `Dark`: the diagram-design skill's default skin, profile
  `codetospec` in `.diagram-design`, translucent roles composited: D2
  takes no alpha, and explicit colours don't follow its dark theme, so
  dark is a second source), only the classes used, ELK layout (the
  skill's right-angle connectors). `Renderer.Draw` runs codetospec-d2
  (`FindRenderer`: a path, or beside the executable, then `PATH`) on a
  batch, JSON over stdio, both palettes; no renderer or a failed
  diagram keeps the source (a picture never fails a run).
  `Picture.Files`/`Markdown` own the file names (`name.d2`, `name.svg`,
  `name-dark.svg`) and the README `<picture>`. `TestRendererTestdata`
  keeps renderers/d2's test sources equal to `Source`'s output.
  The output README draws two diagrams, never a "domain map" (packages
  are code, not business domains,
  `docs/verification/concept-regrouping-2026-11-10.md`): the package
  dependencies (`internal/packagemap`: a node per `package` fact, the
  Go extractor's import paths, else per module fact (a namespace); an
  edge per import resolved exactly, else to the longest package
  prefixing it; shared-root packages drawn external, only when imported;
  labels without the prefix all share; unconnected packages in one
  grid, `d2.GridIsolated`: ELK lays them in one row otherwise) and the
  concept map (`domainmodel.ConceptMap`: a frame per model, entities
  with their placed rules, owns / references / is-a edges, other
  models' entities external; of the confirmed models, else of the
  proposal `domainmodel.Proposal` reads from Enhance's output, which the
  run now builds even without `--write-domain-model`, titled
  "(proposal)" and introduced as unconfirmed). Built in
  `pipeline.modelsAndDiagrams`, drawn in one batch in `pipeline.finish`,
  written under `<out>/diagrams/` (`ReplaceDir`: no stale SVG). ELK's
  time grows faster than the edges (182 edges 16 s, 274 47 s, 358 97 s,
  378 two minutes, 1117 over ten): `Draw` sends no diagram of more than
  `d2.MaxEdges` (300); it keeps its source.
- `internal/policyview` — `export --format views` (`--lang`): the
  access matrix (`index.html`, `page.html` embedded: markup only, words
  from `locale.Access`) of every persona on every exported operation and
  item kind, decided by `cedarpolicy.Decider`, personas decided alike
  merged into one column (`Views.merge`), operations the exports leave
  out listed with the skip reasons; and a decision diagram per guarded
  operation (`operations/<slug>`: each guard's `Expr` as questions,
  yes/no edges, in rule order; the one focal node is the allowed end,
  never a denial). Pure until `Views.Files`; `export` draws and writes.
- `internal/nfrtarget` — a person's answers to `nfr_target`:
  `<out>/targets.yaml` (never written), keyed by rule id (a question id
  hashes the cited code; a rule id survives edits and renames), each one
  of `measure` (Planguage scale, meter, must/plan), `policy` or `example`,
  with the `digest` it was set for. Loaded by `render.LoadPrior` (a
  malformed entry stops the run before any LLM call); `Annotate` (in
  `judge`, after `AnnotateThresholds`) sets the NFR's `threshold` (over a
  stated one: the person's target is the requirement, and the question
  closes) and `target`; another digest → `target_stale` →
  `stale_target` question. A target with no NFR of its id is counted in
  the README, never an error. `export` passes the file to `quire.Export`.
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
  requirement, or overlapping citations with Jaccard ≥ 0.6 or same id;
  across domains the citations must overlap, and a same-domain pair wins
  a tie) keeps its id. A moved rule's id keeps its old domain slug: read a
  rule's domain from its `belongs_to` edge (`graph.RuleDomain`), never from
  its id. Extractor rules take their id first; an
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
  (`Ledger.ResolvedRule`, `ResolvedGap`, `ResolvedDivergence`,
  `ResolvedLink`). `List` takes a `Findings` (nodes, gaps, divergent
  pairs, column links). `column_link` (`extract.ColumnLinks`, pure over
  table facts): a column outside its table's keys named as the single
  column of a foreign key another table declares, "T.c → U", never
  asserted; listed under the README's model files; id without digest.
- `graph.AnnotateCoverage` — `coverage: complete | partial` per domain,
  with `coverage_gaps` (generated file, failed map chunk, failed reduce),
  computed by `coverageGaps` in `pipeline.go`; "Code without rules" section
  of the output README. A path that is excluded or has no grammar has no
  domain: it never makes a domain partial.
- `internal/atomicfs` — the only writes of the output: `WriteFile`
  (`github.com/google/renameio/v2`: temp beside the target, fsync, rename;
  Unix only, as the cgo build already is) and `ReplaceDir(dir, files)`
  (`nodes/`, `models/` written into a hidden staging sibling, unsynced,
  then swapped; no files = no dir); an interrupted render leaves the
  previous output intact. `onlywriter_test.go` parses every non-test Go
  file of every module and fails on any other `os` call that writes,
  renames or removes (the shared `.golangci.yml` stays untouched).
- `internal/locale` — all fixed output strings, FR and EN side by side
  (`tr(fr, en)`), including the EARS templates of the prompts. Writers take
  `locale.For(--lang)`; parsers (`graph.RuleRequirement`,
  `graph.RuleCriteria`) accept the labels of every language.
  `onlylocale_test.go` fails on a prose literal (two lowercase words) in
  the spec writers (`render`, `questions`, `domainmodel`), error and log
  messages and struct tags aside. Exempt: `render/llms.go` (llms.txt,
  one language by decision) and the exports (`quire`, `kiro`: the text
  their pinned formats require, kept in the one package that knows each).
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
  are not in `promptKey`). `graph/journeys.go`: from the same handler
  spans and the direct `call` facts (≤ 3 deep, `dispatch: interface`
  ignored as for drift), each endpoint lists the rules reached, by depth
  then first citation then id: frontmatter `journey`, numbered body list.
  An endpoint field, not a node type: a journey has one entry point, and
  the code shows no sequence of endpoints. Handler spans come from the Go
  extractor's routes and the webapp extractor's GraphQL resolvers (its
  route facts' `handler_lines`,
  `docs/verification/resolver-journeys-2026-11-06.md`).
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
  the output README. `Of` is the partition (file → community, numbered
  by smallest file), `Splits` reads it, so the alignment shares one run.
  gonum is pinned: a version bump can change the communities. Pitfall:
  gonum walks the graph's nodes and neighbours in the order the graph
  gives, and a `simple` graph gives its maps' order: ties broke
  differently on each call (assignments: 9 or 10 communities from one
  input). `sortedGraph` iterates by id; `TestOfDeterministic` fails
  without it.
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
- `internal/position` — positional nature, no LLM: codetospec.yaml
  `nature_by_path` (per nature, Go-style package patterns relative to
  `--src`: `internal/http` = that directory, `cmd/...` = the tree; most
  specific wins) or `nature_by_layer` (nature of each layer of
  `<src>/anarch.json`, import paths cut by `go.mod`'s module; the two keys
  exclude each other, checked before any LLM call, `cmd/.../position.go`).
  A rule whose every citation under `--src` is in layers of one nature,
  different from its own, gets `nature_by_position`. Every rule with
  structural evidence (no words of its text: business, `touches`,
  `exposed_by`, a condition variable on an entity node's table or a
  `--domain-model` entity; technical, a quality attribute other than
  security, safety, compliance, usability, functional suitability, or
  platform code: `inherited_from`, or citing only another root) gets
  `nature_evidence` and `nature_by_evidence` (business, technical, or
  `open` with both; DOMAIN_PLAN.md rec. 4, 94 % agreement with
  `scripts/domainsplit.py` where both decide,
  `docs/verification/nature-by-evidence-2026-11-04.md`), and
  `facets: access` when it serves security and checks something
  (extractor rule or structured condition). A `nature_mismatch` question only when that
  evidence backs the position (`position.Asks`); its `nature` never
  changes (measured: the text is the better reading more often,
  `docs/verification/positional-nature-2026-11-01.md`; 226 → 49 questions
  on districts, `docs/verification/nature-evidence-2026-11-04.md`). Each
  layer's placed / disagreeing / asked rules: "Layers" section of the
  output README (`position.Layer`), a check on the mapping.
- `internal/concept` — with `--domain-model` (a person's confirmed
  models), `concept.Assign` places each rule whose `nature_by_evidence` is
  business in a concept (`domainmodel.Concepts`: model entities, the code
  entity each matched, owners from `owned` relationships) from its
  `touches` edges and condition variables on a concept's table; several
  concepts go to their nearest common owner, none in common is
  `concept_conflict` (a question, keyed by the options). Frontmatter
  `concept`, `concept_evidence`. `concept.Regroup` then applies it (only
  confirmed models reorganize the output): each placed rule a person
  didn't review moves into its model's context domain (the model's
  scope, `-context` when a code domain of that name keeps rules), its
  rules listed under their concepts; the context depends on the
  contexts its model imports (`domainmodel.Contexts`). The move itself
  is `graph.Move`: belongs_to, every touched domain's rule list (the
  body helper `Build` uses), the target's coverage (partial with its
  sources' gaps) and depends_on (declared, plus its sources' services),
  emptied gapless domains dropped with the edges to them, rule ids
  unchanged. README "Concepts" = rules moved per context and where they
  came from (`docs/verification/concept-regrouping-2026-11-10.md`:
  assignments, 786 rules into one context, on its proposal taken as
  confirmed). Runs right after `position.Annotate` (`pipeline.place`). `concept.Subjects`: an actor's
  `owns` (config, `domainmodel.CheckOwned` before any LLM call) makes it
  the EARS subject of the rules of its concepts (`graph.WithSubject`
  re-renders the sentence; reviewed rules untouched; runs after the
  crosscheck, which judged the sentence as the reduce wrote it).
  `concept.ConflictSets` (in `Report.Sets`): the conflicts' concept sets
  in the greedy order that settles the most soonest (answering a set
  settles every conflict whose concepts it contains), listed in the
  README's "Concepts" section.
- `internal/alignment` — with `--domain-model`, `alignment.Measure` (in
  `buildMeta`, pure, one entry point) compares the confirmed models with
  the code, no LLM: each unordered pair of concepts (a model relationship
  is structure, not a call direction) converges, diverges (a
  `concept_divergence` question, id without digest: about the model),
  is absent, or unobservable (one concept with neither placed rules nor
  a table: unknown, not absent). Evidence from facts only: a foreign key
  between the concepts' tables, a direct call (`extract.DirectCalls`, no
  interface dispatch) from code one concept's placed rules cite into
  code another's cite; imports left out (a package serves most
  concepts). Also scattering (per concept, the directories its rules
  cite) and tangling (per directory, the concepts), rule counts beside
  them (size), and `Recovery`, the adjusted Rand index of the
  communities against each file's majority concept. Declared relations:
  `domainmodel.Relations` (every relationship and is-a link, qualified).
  README "Concepts and code"
  (`docs/verification/concept-alignment-2026-11-11.md`). The placement
  itself rests on `touches` (reduce) and condition variables: the report
  is deterministic given it.
- `graph.SharedEntities` — entities touched by rules of several domains
  (`touches` links from the reduce), "Shared entities" section of the
  output README: shared kernel or misplaced boundary.
- `internal/domainmodel` — comparison with modelith models written by
  humans (`--domain-model`, read only, no LLM), via
  `github.com/StevenACoffman/indomain` (importable fork of modelith,
  admitted into the main module). Lint first: an error stops the run
  before any LLM call. Entities matched by name (normalized, regular plural
  accepted, or `domain_model_entities`), invariants ↔ rules that touch the
  entity by Jaccard (threshold 0.3, EARS template words removed), model
  actions ↔ the rules' recorded EARS actions by first verb
  (`domainmodel/actions.go`; negation and "only" skipped, camelCase split,
  an unknown action never guessed); section
  of the output README. Outside `promptKey`: no cache invalidated.
  `--write-domain-model`: `domainmodel.Enhance` (pure, returns the files)
  writes under `<out>/models/` the enriched human models (yaml.v3 nodes:
  comments and order kept; definitions E → C → A, the placeholder prefix
  makes it recognized on the next round) and one proposed
  `generated/concepts.modelith.yaml` (`domainmodel.ConceptsFile`) for the
  tables and owned `api_type`s no model names, `owned` relationships from
  NOT NULL foreign keys to a single parent (DOMAIN_PLAN.md rec. 1, 2, 10;
  never one model per current domain), plus `provenance.json`. A written model read back as human does
  not change (round-trip test). Table entities singular (`singular`, the
  inverse of `plurals`, so they match back; API types and enums keep
  their names); an owned key equal to the child's `primary_key` is
  `1:0..1`; a type implementing exactly one owned interface gets
  `subtypeOf`; a column whose code type names an `enum` fact (bare name
  in the table's directory, or `pkg.Type`) is typed by it, written under
  `enums:`; `column_descriptions` describe attributes; `stored_by` names
  the Go type in a kind's placeholder. Definitions from the code's
  documentation are drafts (`locale.DraftPrefix`, `isDraft`), kept as
  written. `Entity.Unreviewed`/`Report.UnreviewedEntities`: a
  `--domain-model` model still holding placeholders or drafts is flagged
  (WARN, README "Concepts"), never refused
  (`docs/verification/proposal-from-code-2026-11-11.md`).
- `cmd/codetospec/main.go` — the full sequence.

## Extractors and the D2 renderer = SEPARATE Go modules

`renderers/d2/` (binary `codetospec-d2`, depends on
`oss.terrastruct.com/d2`, whose ELK layout runs in an embedded JavaScript
engine) draws D2 sources as SVG for `internal/d2`, protocol pinned in
both package docs; Geist (`fonts/`, OFL) embedded; `OmitVersion`, so a
rebuild against the same layout gives the same bytes. Its dependencies
were bumped past govulncheck's called findings (`x/net`, `x/text`,
`goldmark`).

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
longer emits them) and `call`, and, for the policy inventory, what each
function reads: `reads` (struct fields), `uses_value` (package-level
constants and basic-typed variables, with type and value: capabilities,
flags), `loads` (datastore kinds and filtered properties),
`calls_method` (module methods called, `called_method`), `package`
(each package's import path, the analyzed module's and the shared
packages it imports: what the package map resolves imports by), plus
`tags` on struct facts (`docs/verification/check-inputs-2026-11-06.md`).

`api_type` (webapp: federated `@key` types, `owned` or extended,
`deprecated`, `description` from the docstring, `implements`; plus the
interfaces an owned type implements, `interface: true`) and the `table`
facts' `references` (Go extractor: foreign keys, `not_null` = composition
evidence), `primary_key`, `description`, `column_descriptions` (SQL
comments; webapp: Datastore field comments) and `stored_by` (webapp: the
Go type of a kind named otherwise) are concept seeds (DOMAIN_PLAN.md rec.
7, 10). The Go extractor reads the `.sql` files in walk (name) order as
one schema (`extractors/go/sql.go`, regexp-based on purpose: dialects and
comments): ALTER TABLE (columns added, dropped, renamed, retyped, NOT
NULL set or dropped, keys added), RENAME TO, DROP TABLE; `.down.sql`
files and goose/dbmate down sections never run; a table cites the file
creating it.
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
(`extract.DomainHints`, taking priority over the domain strategy).
`--exclude` applies to them too: an extractor walks with its own skip
list, so `pipeline.extract` drops a rule or domain fact citing a path the
walk skips (`extract.WithoutExcluded`, `source.Excluded`; counted in the
README). The webapp extractor also names what each guard checks
(`checks`, `check_args`: the operands the condition notation drops,
`delegates_to`), one `access_check` fact per check named, and
`unguarded` for a resolver with none (contract: `extract.KindAccessCheck`,
`KindUnguarded`, `AttrChecks`…; read by the policy inventory, not the
spec; `docs/verification/policy-checks-2026-11-06.md`). It reads a
marked checker's decision from its declared results (the first `bool`,
wherever it stands; a nil result for one with none; an empty filter
result its caller denies on), follows checkers wrapped in a package
variable and resolver packages the root resolvers embed, and names every
function a denying guard calls (`form: call`, no meaning) so a person can
pin it (`docs/verification/checker-recognition-2026-11-06.md`). A
checker's allowing paths (`paths.go`): `return nil, nil` from a `(*T,
error)` checker denies; a `(bool, error)` one denying with an
authorization error and never by its bool alone decides by its error
(`errorDecides`); a collection first result is a filter whatever follows.
It also recognizes capability helpers (`caphelpers.go`: a function whose
decision is admin's `hasPermission` field in its genqlient operation;
calls get `means: capability`, scope from the enum, the capability by Go
name from admin's generated `capabilities.go`; `hasContentPermission`'s
scope is `content_args`, which no pin states), binds the request user's kaid to
`from: caller`, makes no guard of a branch that returns `true`, and
reads a branch returning `RequestUser()`'s error (alone or in `||`) as
rejecting service-admin requests, unless the path already tests for them
(it also fails on an unvalidated identity, which both loader contracts
deny). The Cerbos suite leaves out assignments no single principal kind
realizes (`policy.Realizable`, the `kindsOf` that `Principals` uses). No
repository-specific code in the core: all webapp knowledge lives in
`extractors/webapp`.

## Cache & resume (`<out>/.codetospec/`)

- `map/<chunkID>.json` — name = hash of the chunk content; reused if its `prompt_key` matches. The domain of a cached output is taken from the current chunk (changing the domain strategy regroups without re-mapping).
- `reduce/<domain>.json` — reused if the hash of the candidates (`candidates_hash`) matches.
- `crosscheck/<ruleID>.json` — key = hash (prompt key + rule + cited lines).
- `acceptance/<ruleID>.json` — key = hash of the model and the whole
  conversation (prompt templates, rule, condition, cited code, tests); the
  observable's review (`--crosscheck`) under its own such hash.
- `quality.json` — a single file, answer by hash (prompt key + title +
  requirement); keeps only the run's rules.

Every key includes the model and the **text of the prompt templates**
(`llm.PromptKey`, language included when the prompt depends on it, plus
`--repair` for the crosscheck; the acceptance key hashes the whole
conversation). Editing a prompt or changing the model therefore invalidates the cache on its own — no version
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
