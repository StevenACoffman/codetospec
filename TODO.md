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

## Acceptance

- [x] Add a journey node that orders endpoints and rules into a path someone
      can run end to end, as the starting point of an acceptance test or a QA
      agent. Rules, entities and endpoints exist; nothing groups them into a
      user journey. Candidate sources: call facts from a handler through its
      callees, and endpoint sequences an extractor can see.
  - Done: as an endpoint field, not a node type (one entry point; the code
    shows no sequence of endpoints): `journey` frontmatter and body list,
    from handler spans and direct call facts (`graph/journeys.go`).

- [ ] Record the two acceptance facts EARS does not carry: how an observer
      tells success from a plausible imitation, and what the rule does not
      claim. The EARS components already give the starting state (`while`),
      the trigger (`when`, `if`) and the behaviour (`shall`). The Kiro and
      Quire exports would carry the new facts as criteria.

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

- [ ] Share one modelith reader with anarch-lint. codetospec reads models
      through `indomain`; anarch-lint has its own `internal/modelith` adapter,
      so every model feature, actions included, is built twice. Decide whether
      anarch-lint can depend on `indomain`, or both on a shared package.

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
