# TODO

Outstanding work for codetospec found by comparing it with the
harness-engineering notes (`~/Documents/git/harness-engineering/docs`), with
anarch-lint (`~/Documents/git/anarch-lint`) and with the NLP4RE tools catalog
(`nlp4re-tools.md`), plus what the policy work left open. `IMPROVEMENTS.md`
keeps the survey of neighbouring projects and its measurements; this file
holds the rest.

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
- [x] Measure run-to-run variance after the next remap. Remap one output
      (pkg) twice with the same prompt and model into two `--out`
      directories, then compare candidates per chunk and by nature, and
      the rules `diff --old --new` reports removed or added. Folded into
      `scripts/remap.sh` (the second pkg run into
      `~/spec-graphs.variance/pkg`, `scripts/remapcheck.py` for the
      comparison): record its report as a `docs/verification` file. Decide from
      it how large a change in rule counts (the corpus baselines, a
      prompt change's A/B) is a real one. The 4-reply ablation on 60
      chunks gave a per-chunk sd of ~0.5 rules; a whole run's is unknown.
  - Also check the new prompt on the full run: technical candidates back
    near 786, small chunks not inflated (the ablation gave +29 % rules
    on chunks under 100 lines). Section 2 of the same report, against
    the snapshot and `~/spec-graphs.before-remap-2026-11-02`.
  - Measured (2026-11-05, `docs/verification/remap-2026-11-05.md`):
    whole-run totals move 0.4 % (about 2 % within a nature), so a count
    change beyond a few percent is real; but about half the rules have
    no match between the two runs, so compare counts, not rule lists.
    Technical candidates 907–926 against 786 before the loss; rules on
    chunks under 100 lines +24–31 %, as the ablation predicted.

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
## Business Concepts (`DOMAIN_PLAN.md`)

- [x] Keep rule ids across a domain move (recommendation 25):
      `graph.MatchRules` matches across domains when the cited code
      overlaps (`docs/verification/cross-domain-ids-2026-11-04.md`).
- [x] Split subject matter from technical by structural evidence
      (recommendations 4, 13, 19): `nature_by_evidence`, `facets: access`
      (`docs/verification/nature-by-evidence-2026-11-04.md`).
- [x] Concept seeds from code (recommendations 7, 10): `api_type` facts
      (webapp), foreign keys on `table` facts (Go extractor).
- [x] One proposed concepts model instead of one per domain
      (recommendations 1, 2, 10;
      `docs/verification/concept-proposal-2026-11-04.md`).
- [x] Concept assignment from a confirmed model, recorded and sized
      (recommendations 3, 12, 15;
      `docs/verification/concept-assignment-2026-11-04.md`).
- [x] Actors that own concepts are the EARS subject (recommendation 23).
- [ ] Apply the concept regrouping: move each rule with a `concept` into
      its model's context (domain), on a person-confirmed model. TT-RecS
      (`nlp4re-tools.md` item 10) is a reference for scoring candidate
      concepts with a person confirming. Measure
      first on the assignments pilot: coverage, conflicts, and id churn
      with cross-domain matching
      (`docs/verification/concept-assignment-2026-11-04.md`: 625 of 794
      business rules placed, `Assignments` 494 from ~20 domains). A domain
      node carries the reduce summary, coverage, `depends_on` and the
      per-domain exports: all must follow the move.
- [ ] Variants (`DOMAIN_PLAN.md` recommendation 17): revisit when a
      confirmed model names the dimension; code enums find 8 of 67 pairs
      (`docs/verification/enum-variants-2026-11-04.md`).
- [ ] Pilot on assignments with an expert (recommendations 26, 27): have
      a person correct `generated/concepts.modelith.yaml` (definitions,
      taxonomy, the change-event kinds the 11 conflicts name, vestigial
      Datastore models), then re-measure assignment on it. Every
      placement number so far used the unreviewed proposal.
- [ ] Measure the EARS subject change (recommendation 23) on a real
      output with an `owns` glossary: rules rewritten, `ears_issue`
      count, MatchRules keeping their ids on the next run.
- [ ] Concept evidence from endpoints (recommendation 12): rules reached
      by the journeys of an endpoint whose type the model owns. Today
      only `touches` edges and condition variables place a rule.
- [ ] Declared vs computed (recommendation 16): let a person declare the
      concept of a file or a rule, and ask when the computed one
      disagrees. No place to declare it yet (modelith has none).
- [ ] Taxonomy (recommendation 8): the proposal does not mark is-kind-of
      (the six platform assignment types under Assignment); propose it
      from an assignment-type enum or `BaseAssignment` embedding, as a
      QUESTION for a person.
- [ ] Terminology checks (recommendation 9): one concept under several
      domain names (`content-assignment` / `content-assignments`), one
      name for several concepts. The concept table shows the first;
      nothing reports it. Method: REVV's near-synonym detection over the
      rules' `touches` and condition variables (IMPROVEMENTS #17), as a
      question kind.
- [ ] Termhood for concept proposals (IMPROVEMENTS #17): score
      identifiers by how specific they are to this repository against
      every graph under `~/spec-graphs` (C-value, weirdness), so
      `domainmodel.ConceptsFile` proposes domain entities, not
      infrastructure. Measure on the assignments proposal the pilot
      reviews.
- [ ] Audit facet (recommendation 5): change events and audit trails
      have no structural signal yet; only `access` is a facet.
- [ ] Boundary error handling as platform (recommendation 6): the 70
      subject → technical disagreements of
      `nature-by-evidence-2026-11-04.md` are mostly "IF retrieving X
      fails" inside business flows; find a structural signal (EARS
      `if` trigger + responses that only propagate or log) before
      deciding.
- [ ] Cruft (recommendation 21): deprecated API types are only named in
      the proposal's description; make them, and conditions on
      hard-coded ids or flags, "still needed?" questions. One-off `cmd/`
      commands: next item.
- [ ] Ask "still needed?" about one-off `cmd/` commands (`DOMAIN_PLAN.md`
      recommendation 21): backfills, purges, seeds (`CLASS-15005-purge`,
      `dist-4399-backfill`). Telling a one-off command from a lasting one
      (`validate`, `rosterutil`) needs a convention the code does not
      state; decide one first.
- [ ] Ownership from code paths (recommendation 10): delete and archive
      paths, and `ALTER TABLE … REFERENCES` (not read by the Go
      extractor's `sql.go`).
- [ ] Lifecycle as a concept or facet (recommendation 11): 232
      assignments rules in 30 domains mention due, archived, scheduled,
      draft.
- [ ] Layers by spread across concepts (recommendation 14), split/merge
      by measured distribution (18), per-service bounds (20), thin
      services (22), roles in modelith `actor` from guards (24): need a
      confirmed model and the pilot first.

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

- [x] Ask `nfr_target` only for an NFR that states or names a quantity
      (a number, or a timeout, limit, TTL… the cited code names), not for
      every technical rule with a quality attribute. Let a threshold come
      from a named constant the cited code reads (the `constant` facts,
      as grounding maps `400` ↔ `http.StatusBadRequest`), beside the
      numbers stated in the requirement.
  - Done (2026-11-06): 3,732 → 20 questions on the corpus; a constant
    threshold excludes enum values
    (`docs/verification/checker-definitions-2026-11-06.md`).

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

- [x] Re-record the corpus baselines: `make corpus` skips every entry
      since webapp moved past the baselines' commit (`88b55ce5c33` →
      `bfad3957bf1`), so it has compared nothing since. Folded into
      `scripts/remap.sh`: it re-records them after the remap and shows the
      manifest's diff (old counts against new) for review before commit.
  - Done (2026-11-05): webapp `c55b04b0090`; rules +26–44 % per output,
    share proven unchanged, no violations, drift or `ears_issue`
    (`docs/verification/remap-2026-11-05.md`).
- [ ] Regenerate `~/spec-graphs` and re-record the corpus
      (`scripts/remap.sh`). The outputs predate FIX R1-R3 (admin still
      carries the leaked `testutil` role rule, and the corpus baseline
      counts it) and the checker recognition (new `check_args`,
      `access_check` and route facts, and changed extractor conditions,
      which re-map the chunks they feed). Until then, `make corpus`
      measures stale outputs. Review the baseline diff before committing.
- [ ] Fix what checking admin's role rules against their code found
      (2026-11-05): findings, recommendations and the step-by-step plan
      are in `FIX.md`. The 46 role rules are exact, but `--exclude` doesn't
      reach extractor rule facts (a `testutil` role and domain leaked in),
      and tests that name a package-level `var` reach nothing, so every
      role rule says `tests: 0`. The remap has finished, so it can start.
      Only the exclusion fix moves the corpus (admin loses its leaked
      `testutil` rule): one reviewed re-record after it. The corpus
      doesn't count `tests`. `policy_inventory_plan.md` builds on the same
      package-level `var` index (its Part 2 design 8, after FIX R3).
  - Done in the working tree (2026-11-06): R1 (`source.Excluded`,
    `extract.WithoutExcluded`), R2 (webapp `-exclude-dirs`), R3
    (`extractors/go/vars.go`; 50 admin rules gain tests,
    `docs/verification/testreach-package-vars-2026-11-06.md`). R4 done
    (2026-11-06: the role sentence names the grant's scope; re-run on a
    scratch copy: 46 role rules reworded, ids kept,
    `docs/verification/quality-promise-and-admin-rerun-2026-11-06.md`;
    `~/spec-graphs/admin` itself waits for the remap). Open: R5-R6, and the corpus re-record
    once the outputs are regenerated.
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

## Measurement Against Outside Data (`nlp4re-tools.md`)

- [ ] Measure `--quality` on labeled requirement sets (IMPROVEMENTS #14).
      Attribute half done (2026-11-06): 83.4 % accuracy, detection
      precision 92.0 % and recall 96.3 % on the relabeled PROMISE set
      (`docs/verification/quality-promise-and-admin-rerun-2026-11-06.md`);
      left: confusions per class (the record keeps counts only), repeats
      for flips, and the nature check. The original item:
      Dalpiaz's RE-2019 sets and NoRBERT's PROMISE splits, downloaded at
      evaluation time (GPL-3.0), through an eval harness modeled on
      `CODETOSPEC_EVAL`. Decide the PROMISE `O` (operational) mapping
      first. Also check nature on the sets labeled both functional and
      quality: `technical`, `business` or `open`? Add the ZSL4REQ
      baseline only if the result is ambiguous.
- [ ] Check condition logic against the EARS text with CiRA
      (IMPROVEMENTS #15): an offline script over one `~/spec-graphs`
      output, counting rules whose AND/OR/NOT structure disagrees with
      their condition while the values agree. If the count is
      meaningful, decide whether it becomes a question kind beside
      `condition_mismatch`.
- [ ] Add NL2RT's EARS specifications and MAANA's coordination cases as
      `ears` test inputs (IMPROVEMENTS #18); use MAANA's method on the
      lowercase-"and" trigger case. Measure a vagueness check's precision
      before adding one.
- [ ] Measure citations against TAROT's gold links (IMPROVEMENTS #16).
      Blocked on a Java grammar in `internal/sitter`; start with iTrust,
      at file level.
- [ ] Read the Ross-taxonomy business-rule papers (Anish et al. 2019,
      2022) before the next change to rule nature or concepts, and the
      system-states paper (Pudlitz et al. 2019) before the next change
      to `enum` nodes.

## Policy Inventory and Exports

Left open by `policy_inventory_plan.md` and the checker work
(`docs/verification/checker-recognition-2026-11-06.md`). Never
over-permit stays the rule: an operation the export can't decide whole is
left out.

- [ ] Cross-service join of inventories: built (`--join`, `policy.Join`)
      for a target check about no user. Left: a target check about a user
      needs to know which of the call's arguments is that user.
  - Decided (2026-11-06): run the join across the reviewed graphs first
    and count the checkers whose target check is about a user. A handful:
    a person pins the mapping (`join_subject_arg`). Most, with static
    queries: the extractor maps the GraphQL variables at the call site.
    Never match parameters by name (a wrong match can over-permit).
  - Measured (2026-11-07): across six re-run graphs the join reaches 2
    checks, both "the operation answers no check". It is blocked
    upstream, not by the user mapping: `dependency` facts name the root
    field a query starts from (`users:Query.user`, 36 of ai-guide's 119),
    not the federated field that decides, and several cross-service
    checkers aren't recorded as checks. Next: dependency facts that name
    the answering field; the mapping decision waits for that.
- [x] Two-subject operations: two bound users built (`subject2`, self,
      coach and subject relations about the caller). The reviewed
      two-subject checkers check a relation held by a named user who is
      not the caller.
  - Decided (2026-11-06): first check whether those `User.can*` field
    resolvers return the check's result as their value with nothing else
    gating them. If so they are answers (`answers`), not guards. A leaf
    for another user's relation only if a real guard of that shape turns
    up.
  - Done (2026-11-07): the five districts `User.can*` resolvers are
    answers (their callees fail only on a failed load, their rules gate
    nothing). Their guard call sites pass the caller's own kaid, so the
    webapp extractor binds `<local>.Kaid` of a top-level `RequestUser()`
    local to `from: caller`, and a pin case keys on it with `when_arg:
    {"N": "@caller"}`. No guard about a second user turned up.
- [x] A call-level reading for item filters. Three districts guards use a
      visibility filter's failure as a call-level check, which an item
      pin with `self`/`coach` can't state.
  - Decided (2026-11-06): an item entry may carry an explicit call-level
    checker (`call: {…}`), pinned by a person beside the item meaning and
    verified the same way; without one, the operation stays out.
  - Done (2026-11-07): two of the three guards were extractor misreads
    (a branch returning `true` from a bool resolver allows; it is no
    longer a guard). The third is a real call-level use, so `call` is
    built (`Checker.Call`, `atCall`), worded and compared; the visibility
    filter is `self`/`coach` with a call-level meaning, and the three
    operations are kept.
- [ ] Path coverage: read more of the code before flagging a pin. A
      review of the 13 flagged definitions (users, coaches, ai-guide,
      districts) found 10 pins correct and the flags due to these limits,
      in order of how many they explain:
  - [x] reuse the pins of the checkers a path calls, and inline a package's
    private helpers (`_checkActorCapabilityScopes`,
    `_actorCanViewAllAssignmentDistrictInfos`, an unpinned wrapper of the
    coach lookup). Done (2026-11-07): a call of a check with no meaning
    and no pin inlines that check's own paths (`pathReader`, depth
    `MaxDepth`, cycles cut);
  - make checks under `any` and `not(any(…))` readable: split `a || b`
    into paths and push negations to the leaves before matching;
  - read the decision from the function's error when it denies with an
    authorization error, and don't take a bool that isn't the decision
    (`AuthorizeCSVExportRequest`'s means "is the current user");
  - treat a nil result from a function returning a pointer as a deny
    (the extractor read `return nil, nil` from a `*bool` as an allow);
  - don't count a `cache.Cache` wrapper as a call level when grounding
    leaves (two capability leaves read 4 calls down looked ungrounded);
  - compare an item filter's pin with its per-item decision, never with
    the function's own bool (`AdministeredSchoolsInDistrict`'s second
    result is a stricter, separate decision callers gate on);
  - [x] follow a check one level into a cross-service helper. Done
    (2026-11-07) for capability helpers: the webapp extractor reads a
    helper whose decision is admin's `hasPermission` from its genqlient
    operation (`means: capability`, scope from the enum, capability a
    literal, a constant or the call's argument). Left: helpers whose
    locale is an argument (`hasContentPermission`);
  - recognize `actor.Kaid == kaid` and the coach-coachee lookup as
    `self` and `coach`;
  - don't count a call that only builds the result (`moaPayload`) as a
    check.
- [x] `checksBelow` can't tell a check returned as data from a deeper
      guard. Done (2026-11-06): the webapp extractor's `answers` (a check
      returned as the resolver's value, its error unable to reach the
      caller); coaches regains one operation.
- [ ] The 34 reviewed checkers still unrecorded, by why:
  - **16 have a pin but no guard of their own.** Most are helpers called
    only from inside checkers the extractor does record (a capability
    helper inside `isActorCoachOrAdmin`, a content-kind helper inside
    `checkEditContentKindPermission`). The rest are results passed into a
    write or a transaction (`canEditDevProps` into `ReviseProgram`).
  - **3 decide in another service** (the cross-service join, above).
  - **About 6 are per-item or per-field filters and data predicates**
    (feedback visibility, assessment access status).
  - **8 are helpers whose results steer a mutation** rather than guard
    it (users' settings ACLs).

  Recording a helper or a steering result as a guard would claim
  decisions the resolver doesn't make. Data-access authorization needs
  the inventory to read it first.
- [x] Pins keyed on argument values: `cases` with `when_arg`. Three of the
      five districts checkers take the value as a literal and were
      re-pinned by two blind readers; the other two take a variable.
  - [x] A visibility filter pinned with `self`/`coach` doesn't translate
        where a guard uses the filter's failure as a call-level check (3
        districts operations); decide what that guard means. Done
        (2026-11-07): the item entry's `call` meaning, above.
- [ ] Content-kind scopes: built (`scope: content`, `content_kind` or a
      literal `content_kind_arg`; loader contract updated). Re-pin the 7
      content-editing checkers (and the 8 request-locale ones with
      `scope: locale`).
- [x] Re-pin the review's checkers files with the vocabulary added since
      (`docs/verification/checker-recognition-2026-11-06.md`) and
      re-measure the six exports:
  - `scope: locale` for the request-locale checkers;
  - `capability_arg` for checkers that take their capability as an
    argument;
  - `self`/`coach` in item filters instead of invented relations;
  - a context `attribute` for feature-flag gates pinned at their
    strictest reading;
  - `unpinnable` reasons for the rest;
  - `digest` on every pin.

  One entry alone took users from 112 to 118 operations. The files are
  private.
  - Progress (2026-11-06): the 20 files converted to JSON (all pass the
    schema; inventories byte-identical); three districts checkers
    re-pinned with `cases` by two blind readers, compared with
    `policies --compare`. `policies --checkers` now shows, per pin, its
    coverage by the code's paths: 26 of 31 covered on users, coaches,
    assignments and ai-guide, 6 of 14 on districts
    (`docs/verification/checker-definitions-2026-11-06.md`). Admin,
    re-run after its role sentences changed: export unchanged (37
    operations, 96/96 `cerbos compile`).
  - Reviewed (2026-11-06): of the 13 flagged pins, none over-permits; 10
    are correct (the flags are the path-coverage limits above), and
    `governingMoa`, `UserAdministersAdminAggregate` (the MOA flag, now a
    context attribute) and `GetAllVisibleUDIs` (`cases` on
    `includeDeleted`) are exact now. The JSON files, generated from the
    review's YAML with every comment carried into `note` fields (or a
    per-service notes file), and the 13 approx pins marked
    `approx: true`, live beside the review (private).
  - Done (2026-11-06): 20 checkers re-read by two blind readers each with
    the new vocabulary (`scope: locale` and `content`, `capability_arg`,
    `cases`, context attributes, `self`/`coach` in an item filter), the
    differences adjudicated in the code: 18 pinned, 2 kept unpinnable
    with a firmer reason. Every recorded checker's pin carries its
    `digest`, and every unpinnable checker an `unpinnable` entry (the
    review's 24 files: 152 pins, 128 unpinnable). Exports: users 112 →
    118 operations; `cerbos compile` OK on all five re-run graphs. One
    item filter keeps its relations until a filter can carry a
    call-level meaning (the item above). The re-read also found that
    webapp's current-locale and content checks accept a grant on every
    locale or kind, so the `locale` and `content` templates now accept
    `"*"` (`policy.EveryValue`) too.
- [x] Permits scoped by principal type. Done (2026-11-06):
      `Operation.Principals`, from the denies; on the five re-measured
      graphs, 423 of 846 permits are restricted.
- [ ] Rate limits as obligations: today a rate-limit guard keeps its
      operation out (52 guards). Exporting it as an annotation would
      allow what the code throttles. That is a policy decision for the
      people running the export: decide first, then build.
- [ ] Registry entries naming methods (`ParseRegistry`).
- [ ] Extractor shapes not followed:
  - a local any-combinator passed to a helper (`_anyPermission`, 20 of
    22 sites), which needs higher-order inlining;
  - import aliases (one case);
  - shared-secret principals (the gateways, no graph);
  - an `item` scope on a filter whose denial is an error passed through
    from another service.
- [ ] A `pin` command: an LLM proposes each pin from the checker's code,
      a blind second model refutes it, and the export's oracle checks the
      result, as the crosscheck does for rules. It writes proposals for a
      person, never the checkers file. Needs its own eval, as the
      crosscheck has.
- [ ] Checker bugs as questions (`checker_comment_mismatch`,
      `dead_branch`): recorded by a pin's author (the checkers entry's
      `note` starts it), reviewed beside the rules.

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

- [x] Narrow `condition_mismatch`. Count a value as present when it
      occurs in a condition variable's name or as a constant a variable
      names; drop the condition side (a condition's values are already
      grounded in the cited code, so it only says the text is vaguer).
  - Done (2026-11-06): 2,525 → 802 questions on the corpus; re-annotation
    now replaces a stale disagreement.
- [x] Have the webapp extractor's conditions carry what its EARS text
      states: a permission check's scope and a rate limit's numbers.
  - Done (2026-11-06) beside the condition rather than in it: rule facts'
    `check_args` carry each check's capability, scope matcher, subject and
    a rate limit's key and numbers, and grounding counts them as stated.
    Extractor-rule disagreements 164 → 98 (districts), 170 → 87 (users);
    what remains is the text quoting a whole call the condition names as
    a variable, the next item's
    (`docs/verification/check-args-disagreement-2026-11-06.md`).
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
