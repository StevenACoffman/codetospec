# TODO

Outstanding work for codetospec found by comparing it with the
harness-engineering notes (`~/Documents/git/harness-engineering/docs`) and
with anarch-lint (`~/Documents/git/anarch-lint`). `IMPROVEMENTS.md` keeps the
survey of neighbouring projects and its measurements; this file holds the
rest.

## Map Input

- [x] Exclude test files from the map by default for the languages whose
      extractor indexes them as evidence. `config.DefaultExclude`
      (`internal/config/config.go:113`) lists only directories, so every
      `_test.go` file is mapped by the LLM, although the Go extractor's
      `test` facts already link each test to the rules it reaches
      (`internal/testreach`). Only `scanwebapp.sh:122` passes `*_test.go`.
  - Exclusion already accepts globs, so the default can name `*_test.go`.
  - Keep test files of languages with no test indexing (PHP): excluding
    them loses their evidence.
  - Done: `config.DefaultExclude` names `*_test.go` (Go only); `source`
    test through the real walk.
- [x] Find which map-prompt change lost the technical rules after the
      remap (`docs/verification/remap-rule-drop-2026-11-04.md`: pkg's
      candidates 1,477 → 1,212, technical 786 → 550), and correct it.
  - Measured (2026-11-04): no single change; EARS components and the
    structured condition each cost a third of the technical rules, and
    together drop a quarter of all rules on big chunks. Not truncation.
    Naming observable technical behaviour as a rule restores the old
    count (`docs/verification/map-technical-rules-2026-11-04.md`).
  - Done: the map prompt says so. It changes the prompt key: the next
    run of every output re-maps.
- [ ] Measure run-to-run variance after the next remap. Remap one output
      (pkg) twice with the same prompt and model into two `--out`
      directories, then compare candidates per chunk and by nature, and
      the rules `diff --old --new` reports removed or added. Decide from
      it how large a change in rule counts (the corpus baselines, a
      prompt change's A/B) is a real one. The 4-reply ablation on 60
      chunks gave a per-chunk sd of ~0.5 rules; a whole run's is unknown.
  - Also check the new prompt on the full run: technical candidates back
    near 786, small chunks not inflated (the ablation gave +29 % rules
    on chunks under 100 lines).

## Requirement Classification

- [x] Ask the quality pass about authorization rules. The map prompt counts
      authorization as `business` (`internal/mapper/mapper.go:39`), the
      webapp extractor hard-codes its role rules as `natureBusiness`
      (`extractors/webapp/roles.go:103`), and `technicalRules` sends only
      `technical` rules to the quality pass
      (`internal/quality/quality.go:241-246`). No authorization rule can
      carry `security`, although it is functional behaviour and a security
      requirement at once.
  - Widen the pass to business rules, or only to rules an extractor role or
    the rule's entities mark as access control. Measure with a `--quality`
    run of `scanwebapp.sh` or `scandistricts.sh` before and after:
    `scanwebapp-authz.sh` runs no LLM and never reaches the quality pass.
  - Revisit the package comment's "functional by definition"
    (`quality.go:5-6`): a quality attribute can sit on a functional rule.
  - Done: every rule but presentation is asked; on districts 438/439
    extractor authorization rules and 351 LLM business rules get
    `security` (`docs/verification/quality-business-rules-2026-11-01.md`).

- [x] Derive a deterministic `nature` signal from where the cited code lives.
      A rule whose citations all fall in an adapter, middleware or
      infrastructure package is `technical` by position, whatever its text
      says.
  - Take the layers from the target's anarch-lint `anarch.json`
    (`subsystems`, `layers`, `roles`) when it has one, else from a
    `codetospec.yaml` mapping.
  - Report a disagreement between the LLM's `nature` and the positional one
    as a question; override neither. Measure how often they disagree before
    deciding which wins.
  - Done: `internal/position` (`nature_by_path`, or `nature_by_layer` over
    anarch.json): `nature_by_position` + `nature_mismatch` question,
    nature never overridden; 21.5 % disagreement on districts with a
    careful mapping, text usually right
    (`docs/verification/positional-nature-2026-11-01.md`).

- [x] After the remap, measure how many questions the narrowed
      `nature_mismatch` rules would raise, then decide. Today every
      disagreement is a question: one placed rule in five on districts
      with a careful mapping, mostly answered "the text is right".
  - Per rule: ask only when the positional nature would change an
    outcome, either the quality pass (presentation rules are not asked)
    or the export (only technical rules become NFRs). Keep
    `nature_by_position` in the frontmatter for the rest.
  - Per layer: one question for each layer whose disagreement rate is
    high, to fix the mapping or split a package that mixes concerns
    (a lead for the anarch-lint rule proposals). Choose the threshold
    from the measured rates.
  - Measured (2026-11-04): per rule, 226 questions → 81; per layer, `cmd`
    88 %, `handlers` 94 %, `models` 26 %, the rest ≤ 4 %. Against
    `DOMAIN_PLAN.md`'s evidence, text and evidence agree against the
    position in 128 of 226, almost all in `cmd/` (one-off backfills) and
    `handlers/`: mapping errors
    (`docs/verification/post-remap-measurements-2026-11-04.md`).
  - Decided (2026-11-04): build neither. Fold it into `DOMAIN_PLAN.md`
    recommendation 4 (below).

- [x] Fold `nature_mismatch` into the subject-matter / technical split of
      `DOMAIN_PLAN.md` (recommendation 4). Position becomes one evidence
      signal beside the plan's; a question only when the signals
      conflict, listing each with its evidence (recommendation 15), never
      a majority vote. Each layer's disagreement rate goes in the output
      README as a check on the mapping, not as a question.
  - Decided (2026-11-04): structural evidence only (touched entities,
    endpoints, conditions on an entity's columns or a domain model's
    attributes, a technical quality attribute), no word lists: they are
    English and hand-made. A disagreement is asked only when that
    evidence backs the position; evidence backing the text, or none,
    leaves it recorded (`nature_evidence`) and counted per layer.
  - Done: on districts with mapping A, 226 questions → 49
    (`docs/verification/nature-evidence-2026-11-04.md`).
- [ ] Ask "still needed?" about one-off `cmd/` commands (`DOMAIN_PLAN.md`
      recommendation 21): backfills, purges, seeds (`CLASS-15005-purge`,
      `dist-4399-backfill`). Telling a one-off command from a lasting one
      (`validate`, `rosterutil`) needs a convention the code does not
      state; decide one first.

## Nonfunctional Requirements

- [x] Export NFRs as incomplete proof obligations instead of as FRs. The
      Quire export writes every rule the crosscheck did not refute as an FR
      (`internal/quire/quire.go:76-88`), technical ones included with their
      `quality_attribute` as an extension key (`quire.go:54`); no NFR
      artifact is ever written, because the code states no target or
      threshold (`README.md:267-268`).
  - Export each with its `quality_attribute`, its requirement and its
    threshold marked unknown, and add a `questions.json` entry asking for the
    target. Planguage's Scale, Meter, Must and Plan fields are one
    machine-readable form for the answer.
  - Check whether quoin's spec-artifacts-iso schema accepts an NFR without a
    threshold before choosing the shape.
  - Qualitative attributes such as usability may never get a number: let the
    answer be a reviewer policy or an example.
  - Done: technical rules serving a quality export as
    `non-functional/NFR-*.md`, unknown threshold naming its `nfr_target`
    question; shape checked against the schema
    (`docs/verification/quire-nfr-export-2026-11-01.md`). Planguage
    answers not read back yet.

- [x] Look for stated thresholds in config and code. Timeouts, rate limits,
      retry counts and size limits are often literal values. When an NFR's
      cited lines carry one, grounding already checks that it occurs there
      (`internal/grounding`), so that NFR can export with its threshold.
  - Done: `quality.AnnotateThresholds` (frontmatter `threshold`, from
    `grounding.StatedNumbers` of a grounded NFR); such an NFR exports
    `codetospec_threshold: stated`. Config files are not read.

- [x] Measure whether config files hold the missing NFR thresholds, then
      decide whether to read them. Today a threshold comes only from the
      numbers in an NFR's cited code (`quality.AnnotateThresholds`); values
      set in YAML, environment variables or flags are never read.
  - After the remap, count the NFRs still raising `nfr_target`, and sample
    them by hand: how many have their value in config, and in which form
    (viper, environment variables, struct tags, gqlgen config).
  - If that share is large, have the Go extractor emit `config` facts (key,
    value, `file:line`) and let a threshold come from a config value the
    cited code reads. First decide which value counts when environments
    differ: production, development, or the default in code.
  - Otherwise leave config unread: a person's target answer covers the gap.
  - Measured (2026-11-04): 2,280 `nfr_target` questions, 1,229 distinct
    rules. None of 23 sampled states a quantity, so none has one in
    config: most are technical behaviour classified as a quality. 175
    carry a measurable word; where a quantity exists it lives in code (a
    package constant or variable, a field the caller sets)
    (`docs/verification/post-remap-measurements-2026-11-04.md`).
  - Decided (2026-11-04): config stays unread.

- [ ] Ask `nfr_target` only for an NFR that states or names a quantity
      (a number, or a timeout, limit, TTL… the cited code names), not for
      every technical rule with a quality attribute. Let a threshold come
      from a named constant the cited code reads (the `constant` facts,
      as grounding maps `400` ↔ `http.StatusBadRequest`), beside the
      numbers stated in the requirement.

- [x] Read a person's NFR target answer back into the export. Today an NFR
      without a stated threshold exports `codetospec_threshold: unknown` and
      raises an `nfr_target` question, and nothing reads the answer.
  - Decided (2026-11-02): a separate `targets.yaml` keyed by rule id, not a
    `resolutions.yaml` entry. A resolution is keyed by question id, which
    hashes the cited code, so any edit to those lines would discard a
    target that still holds; rule ids are stable across runs and renames
    (`graph.MatchRules`).
  - Each answer is a typed choice: `measure` (Planguage subset: Scale,
    Meter, Must, Plan), `policy` (a reviewer policy) or `example` (a
    reference case), so qualitative attributes such as usability get an
    answer too. Each type maps to its own Quire field.
  - A stored target fills the `threshold` frontmatter, which closes the
    `nfr_target` question by itself; the export marks it
    `codetospec_threshold: set`, apart from `stated` and `unknown`.
  - Store the cited code's digest with each answer. When the code changes,
    keep the target and raise a `stale_target` question, as inherited rules
    raise `stale_verdict`. Report an answer whose rule no longer exists,
    like a resolution without a question.
  - Done: `internal/nfrtarget` as decided; the Quire export maps a measure
    to the Measurement row, a policy to an Acceptance Criteria row, an
    example to the reference case (validated against the schemas). A
    person's target also wins over a stated threshold.

## Acceptance

- [x] Add a journey node that orders endpoints and rules into a path someone
      can run end to end, as the starting point of an acceptance test or a QA
      agent. Rules, entities and endpoints exist; nothing groups them into a
      user journey. Candidate sources: call facts from a handler through its
      callees, and endpoint sequences an extractor can see.
  - Done: as an endpoint field, not a node type (one entry point; the code
    shows no sequence of endpoints): `journey` frontmatter and body list,
    from handler spans and direct call facts (`graph/journeys.go`).

- [x] Record the two acceptance facts EARS does not carry: how an observer
      tells success from a plausible imitation, and what the rule does not
      claim. The EARS components already give the starting state (`while`),
      the trigger (`when`, `if`) and the behaviour (`shall`). The Kiro and
      Quire exports would carry the new facts as criteria.
  - Decided (2026-11-02), source: a separate opt-in pass after the build,
    shaped like `--quality` and outside the map and reduce caches, so it
    needs no remap. It reads each rule's cited code and its nearest tests
    (`testreach.NearestTests`), has its own cache by hash, and skips
    extractor and inherited rules. Where tests exist, facts derived from
    their assertions seed it; each fact records its source.
  - Shape: structured, `observable: {channel: response | state | event |
    log, check: ...}` and `non_claims: [...]`, written as a locale-labelled
    body section read back like `graph.RuleCriteria`, so a reviewed rule
    keeps them. Grounding checks the values of `check` against the cited
    code or tests; the crosscheck may try to refute the observable.
  - Exports: the observable becomes an acceptance criterion (Quire
    criteria, Kiro N.2 and up). The non-claims become
    `codetospec_non_claims` in Quire (or the schema's own field, if
    spec-artifacts-iso 0.3.0 has one) and go in Kiro's trace comment:
    as criteria they would read as requirements to test an absence.
  - The same pass writes the executable examples of IMPROVEMENTS #5 (see
    Conditions).
  - Done: `internal/acceptance` (`--acceptance`); exports as decided, except
    that Kiro's observable joins the `design.md` property (its
    `requirements.md` takes EARS sentences only). Facts derived from test
    assertions come from the model reading the tests (`source: test`),
    not from a mechanical parser. With `--crosscheck`, a blind review
    tries to refute each observable (`observable_crosscheck`); a refuted
    one gets a question and leaves the exports. On three districts-jobs
    modules, 66 observables, 27
    grounded (`docs/verification/conditions-acceptance-2026-11-02.md`).

## Domain Models

- [x] Match each EARS `shall` action to the actions the modelith model gives
      the rule's entities. The action is already recorded (`shall[].action`,
      `internal/graph/rulefacts.go`); `--domain-model` matches entities and
      invariants but not actions (`internal/domainmodel`).
  - Normalize verbs as entity names are normalized. Report model actions no
    rule performs, and actions on modeled entities that no model action
    names, as leads in the form of the invariant pairings.
  - This files functional rules under the domain model's actions without an
    LLM.
  - Check whether the `indomain` fork reads actions; it may need adding.
  - Done: `domainmodel/actions.go`: model actions ↔ the rules' recorded
    actions by first verb; unperformed actions and unmodeled verbs in the
    README. `indomain` already reads actions.

- [x] Share one modelith reader with anarch-lint. codetospec reads models
      through `indomain`; anarch-lint has its own `internal/modelith` adapter,
      so every model feature, actions included, is built twice. Decide whether
      anarch-lint can depend on `indomain`, or both on a shared package.
  - Found done (2026-11-02): anarch-lint already requires `indomain`
    v0.3.0, and its `internal/modelith` parses through `indomain/pkg/model`,
    translating into its own `arch` types. Decided: anarch-lint depends on
    `indomain`; changes either tool needs go into `indomain`.

- [x] Have anarch-lint recommend `modelith lint` for an invalid model.
      Decided (2026-11-02): anarch-lint does not run `indomain`'s lint (it
      decodes strictly; checking the rest of the format is modelith's job).
      Today only the package comment of `internal/modelith` says so: say it
      in anarch-lint's README and in its error for an invalid model.
  - Done in anarch-lint (`0c3fc95`): `modelith.Parse`'s EINVALID message
    ends with "(check the file with `modelith lint`)", and the README's
    Domain Models section says so.

## Repository Checks

- [x] Enforce the `AGENTS.md` architecture rules that exist only as prose.
      Each failure should name the rule and the repair.
  - "`internal/atomicfs` is the only writer of output" (`AGENTS.md:239-241`):
    an anarch-lint `calls` rule forbidding `os.WriteFile`, `os.Create`,
    `os.Rename` and the like outside `internal/atomicfs`, with one model per
    module because the extractors are separate modules. A forbidigo pattern
    would also work; `.golangci.yml` is a shared copy that is never relaxed,
    so check whether a local tightening is allowed.
  - "All fixed output strings live in `internal/locale`"
    (`AGENTS.md:242-245`): neither anarch-lint nor golangci-lint sees string
    literals, so this needs a small analyzer or a test over the writers.
  - Done: `internal/atomicfs/onlywriter_test.go` (all modules; atomicfs on
    renameio/v2, `ReplaceDir(dir, files)`) and
    `internal/locale/onlylocale_test.go` (spec writers; llms.txt and the
    pinned export formats exempt). The shared `.golangci.yml` is
    untouched.

- [ ] Propose structural rules to anarch-lint. When a technical rule states a
      dependency constraint, such as "package X never calls Y", emit it as a
      candidate `fitness` rule for a person to accept into the target's
      `anarch.json`. No format is fixed yet.
  - Decided (2026-11-02): a proposals file that anarch-lint can read; a
    person accepts each proposed rule into `anarch.json` explicitly.
    Nothing is written into `anarch.json` itself. The per-layer
    `nature_mismatch` questions are a lead for it.
  - Measured (2026-11-02): none of the ~19,000 rules of `~/spec-graphs`
    states a dependency constraint ("only through X", "never calls Y");
    the keyword matches all describe behaviour (a read-only client). A
    mapped rule describes what code does, not how packages depend on
    each other, so rule text is not the source. Deferred until after the
    remap: decide from the per-layer `nature_mismatch` counts (above)
    whether they, or the enum transitions (a `fields` write rule per
    state field), give proposals worth a file.
  - Measured (2026-11-04): neither. The high per-layer rates are mapping
    errors, not dependencies to forbid; 10 enum transitions over every
    output (`docs/verification/post-remap-measurements-2026-11-04.md`).
  - Decided (2026-11-04): re-scope to state-field writers. Have the Go
    extractor report which packages write each enum-typed struct field
    (guarded or not, not only transitions), then measure whether that
    gives `fitness.fields` proposals worth a file, with the rules citing
    the writes as each proposal's reason. A proposal records today's
    writers, not intent: a person judges each.

## Conditions

IMPROVEMENTS #1 (structured `condition` per rule) and #5 (executable
examples) land before the remap. Decided 2026-11-02:

- [x] Add `github.com/faustbrian/go-rule-engine/v2` to the main module: it
      parses, hashes canonically and evaluates the conditions.
- [x] Give the map a vocabulary for condition variables, so an LLM rule and
      an extractor rule about the same check hash equal. Sources, in order:
      the variables of extractor rule conditions, schema facts (tables and
      columns, enums), then the human domain models where present; each
      chunk gets only the terms that appear in it. A variable outside the
      vocabulary is coined under `local.`. The vocabulary stays out of
      `promptKey`, as the actors do.
- [x] When the reduce merges rules: if every merged candidate's condition
      hashes equal, keep it without the LLM; otherwise the reduce writes the
      merged condition, which must parse and ground or is dropped (never a
      run failure). The condition hash is also one more "same rule" signal
      for the reduce's clustering.
- [x] When a rule's condition and its EARS text disagree, neither wins: a
      question, whose detail says which side's values occur in the cited
      code. Never an automatic override; measure the volume and precision
      after the remap.
  - Measured (2026-11-04): 2,133 disagreements; extractor rules 788 (none
    useful as a question: 490 only names the condition carries in a
    variable path, 298 also a check's scope or a rate limit the
    extractor's condition leaves out), LLM rules 924 (condition side 0 of
    19 useful, text side about 1 in 4)
    (`docs/verification/post-remap-measurements-2026-11-04.md`).

- [ ] Narrow `condition_mismatch`. Count a value as present when it
      occurs in a condition variable's name or as a constant a variable
      names; drop the condition side (a condition's values are already
      grounded in the cited code, so it only says the text is vaguer).
- [ ] Have the webapp extractor's conditions carry what its EARS text
      states: a permission check's scope and a rate limit's numbers.
- [x] Executable examples (#5): written by the acceptance pass (see
      Acceptance), not the map, as `{given, expect}` rows checked against
      the condition with go-rule-engine; a contradicted example flags a
      wrong requirement or a wrong condition, with no LLM.
- Done: `internal/condition`, `internal/vocabulary`, the webapp
  extractor's guard conditions, map, reduce, build checks and the two
  questions. On three districts-jobs modules: 45 of 66 rules got a
  condition, none dropped; 5 disagreements (1 wrong condition, 4 vaguer
  texts); 116 of 116 examples hold
  (`docs/verification/conditions-acceptance-2026-11-02.md`).

- [x] Grow the condition vocabulary for Go services. On districts-jobs,
      66 of 69 condition variables were coined under `local.`: those
      modules have no tables, no domain model, and the Go extractor states
      no rule conditions, so map rules cannot hash equal across chunks.
      Candidate source: the Go extractor's types (struct fields as
      `Type.field`), as the enumerations already are (`moa.State` was
      used where offered). Measure the coined share before and after.
  - Done: `struct` facts (module types, and with `-shared` the shared
    types the module reaches), offered as `pkg.Type.Field` where the
    struct is named. Coined variables 62/64 → 39/66, variables shared by
    two rules 7 → 13; 20 of 21 vocabulary uses right
    (`docs/verification/condition-vocabulary-go-structs-2026-11-02.md`).
- [x] Measure conditions on webapp after the remap: its extractor
      conditions (34 in the goldens) are the vocabulary's richest source,
      and how often map conditions reuse them is unmeasured.
  - Measured (2026-11-04): 74 of 6,531 map conditions (1.1 %) name an
    extractor variable, 5 hash equal to one: expected, since the map is
    told not to restate `MECHANICAL_RULES`. 35 % name a vocabulary
    variable, mostly struct fields
    (`docs/verification/post-remap-measurements-2026-11-04.md`).
