# codetospec

Turn a legacy codebase into a versionable, verifiable knowledge graph of its
business rules.

```text
 codetospec  old-legacy-app → ./spec-graph                          deepseek-chat · 8 workers

  RULES TO REVIEW   214 proven  ·  278/312 checked                                    ELAPSED
  34                                                                                    6m12s

  ■ extract  →  ■ map  →  ■ reduce  →  ■ check  →  ▢ render

 ╭─────────────────────────────────────────────────────────────────────────────────────╮
 │ RULES PER DOMAIN   +14 more · 252 total                                             │
 │        billing  ███████████████████████████████████████████████░░░   38             │
 │       shipping  ████████████████████████████████░░░░░░░░░░░░░░░░░░   26             │
 │      inventory  █████████████████████████░░░░░░░░░░░░░░░░░░░░░░░░░   20             │
 │           auth  ███████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░   15             │
 ╰─────────────────────────────────────────────────────────────────────────────────────╯

 ╭─────────────────────────────────────────────────────────────────────────────────────╮
 │ LATEST VERDICTS   214 proven (48 repaired)  ·  32 partial  ·  34 to review          │
 │ ✓ rule.billing.prorata-refund                                                       │
 │ ✓ rule.shipping.freight-surcharge-tiers                                             │
 │ ≈ rule.auth.session-idle-timeout                                                    │
 │ ✗ rule.inventory.backorder-threshold                                                │
 ╰─────────────────────────────────────────────────────────────────────────────────────╯

 tokens  map 1.1M+322k · reduce 189k+88k · total 1.6M+450k      6m12s      [q] quit
```

`codetospec` reads a source repository and produces a markdown graph: domain,
entity, endpoint and rule nodes (requirements written in the EARS format),
linked by typed edges in YAML frontmatter. The output is meant to be read by
humans (GitLab, Obsidian) **and** consumed by agents (`graph.json`,
`llms.txt`) — typically as the specification baseline for a rewrite.

```
---
id: rule.billing.prorata-activation
type: rule
status: generated
sources:
  - path: app/Services/Billing/ProrataCalculator.php
    lines: "42-118"
edges:
  - {type: belongs_to, to: domain.billing}
  - {type: touches, to: entity.invoices}
ears: event
acceptance: 3
nature: business          # business | presentation | technical
origin: explicit          # explicit | implicit
confidence: 0.90
crosscheck: supported     # supported | partial | unsupported | repaired | unchecked
grounding: grounded       # grounded | ungrounded | unchecked
---

# Prorated billing on activation

**Requirement (EARS)**: WHEN a subscriber activates mid-month, the system
shall bill prorated to the remaining days.
...
```

> **New to this problem?** The [Field Guide to Code-to-Spec](docs/README.md)
> is a tool-agnostic walkthrough of the discipline — the principles, the
> [failure modes](docs/03-failure-modes.md) nobody warns you about (the
> line-number trap, reduce truncation, metrics that lie…), and how the state
> of the art fits together.

## The approach

LLMs are good at reading code and bad at being trusted. `codetospec` keeps
the LLM inside a loop it does not control:

- **The program drives; the LLM only does the cognitive work.** Chunking,
  merging, graph building and cross-domain edges are computed
  deterministically. The LLM never invents structure.
- **Every LLM output is mechanically validated** before it enters the graph:
  JSON schema, EARS pattern, citations that must resolve to real files and
  line ranges, references that must exist. Invalid output is sent back for
  correction (twice max), then the unit is marked failed and the run
  continues — one bad chunk never kills a run.
- **Mechanically extracted facts are the anchors.** Symbols, routes, tables
  and imports are extracted by parsers, not by the model. The LLM consumes
  facts; it never produces them.
- **Everything is a citation.** Each rule points to exact `path:lines` in the
  source. `codetospec verify` re-checks a graph against the tree at any time.
- **An adversarial reviewer refutes each rule** (`--crosscheck`): a
  fresh-context LLM sees only the rule and the lines it cites, and votes
  `supported | partial | unsupported`. A disputed rule is flagged, never
  silently dropped; a rule the reviewer never judged is marked `unchecked`,
  so it cannot pass for a supported one.
- **Stated values are checked against the code, without an LLM**: every
  number (`30`, `1 000`, `10 %` ↔ `0.1`, read with the `--lang` decimal
  separator: `1,5` in French, `1,000` = one thousand in English) and quoted
  string in a rule's requirement must occur in its cited lines (a call quoted
  on one line matches the same call split over several lines, or spaced
  differently around operators). The map step checks
  this as it goes: a reply with ungrounded rules gets one correction
  exchange, kept only if it is valid, keeps every rule and grounds more of
  them (cached map outputs are re-asked only with `--reground`, once each).
  A citation that is merely too narrow is widened without an LLM: when every
  missing value occurs at exactly one other line of the file, within 30
  lines of the citation (and is not a lone digit), that line is added and
  listed in `grounding_widened`. A rule that still states a value its
  citation lacks is marked `grounding: ungrounded` and listed in the output
  README, with where the value does appear in the file (usually where the
  citation should point). A rule with nothing checkable is `unchecked`,
  never counted as a pass.
- **Flagged rules are repaired, not just flagged** (`--repair`, needs a SCIP
  index): a rule the reviewer rejected gets one chance to re-cite the exact
  span of a precise symbol that implements it. The new citation must
  overlap a real symbol body, so it is grounded by construction. It is then
  reviewed again, blind: the reviewer never sees the first verdict or why the
  rule was rejected. The repair is adopted only if that review says
  `supported`; otherwise the rule keeps its first verdict and citation, and
  the cached verdict records the re-review (`repair_review`).
- **Rules are classified**, not just listed: `nature` (business /
  presentation / technical) and `origin` (explicit / implicit) separate real
  business rules from plumbing; `confidence` × `crosscheck` gives a triage
  signal.

## Language-agnostic by construction

The core knows nothing about any particular language. Language knowledge
lives in three layers:

1. **tree-sitter** (universal structure): error-tolerant AST for chunking and
   symbols, whatever state the code is in. Grammars are driven by declarative
   `.scm` query files — adding a language means adding a grammar and one
   query file, zero core changes. PHP, Go, JavaScript, TypeScript/TSX and
   Rust ship out of the box; unknown files fall back to line-window chunks.
   Files whose header says `// Code generated … DO NOT EDIT.` (Go's
   convention, before the first code line) keep their facts but are not
   mapped; the output README counts them.
2. **External native extractors** (ecosystem semantics): standalone
   executables speaking a small JSON protocol, free to use the target
   ecosystem's own tooling. Three ship today — PHP/Laravel (routes, schema
   tables, optional runtime introspection), Go (gin/echo/chi routes and SQL
   tables via `go/packages`), and a SCIP converter that turns any indexer's
   output into precise symbol facts. Adding an ecosystem means writing an
   executable, zero binary changes.
3. **The LLM**: reads chunks directly, natively multi-language.

Any OpenAI-compatible endpoint works: a self-hosted vLLM, DeepSeek, or any
other `/chat/completions` provider — pick your model with three environment
variables.

## Pipeline

```
extract (layers 1+2) ──▶ chunk (AST) ──▶ map (LLM, parallel, per chunk)
       ──▶ reduce (LLM, per domain) ──▶ crosscheck (LLM, optional)
       ──▶ build + grounding + verify + render (pure Go)
```

- **map**: one call per chunk extracts candidate rules with citations.
  A chunk is one top-level definition, and adjacent small definitions of a
  file (same namespace and domain) are packed up to 150 lines: each call
  carries several hundred tokens of fixed prompt, so packing cuts calls ~3×
  on a codebase of small functions. When
  precise symbols are supplied (Go extractor, or a SCIP index via the SCIP
  converter), each chunk's prompt is
  grounded with the exact signatures and line spans of the symbols it
  contains, so the model cites real code spans instead of guessing.
- **reduce**: one call per domain deduplicates and consolidates candidates;
  citations must exist verbatim among the candidates — the model cannot
  invent or alter them. A domain with more than `--reduce-batch` candidates
  (default 30) is reduced in batches and merged; a batch that still truncates
  is split in half and retried, so a large domain never loses its rules.
- **build/verify/render**: nodes are assembled and cross-checked (unique
  ids, resolvable edges and citations, frontmatter round-trip). Nothing is
  written unless verification passes, and the render never leaves half an
  output: `nodes/` (and `models/`) is built aside and swapped in, and each
  top-level file is written by rename.

Runs are **resumable** (Ctrl-C safe: state is saved after every unit, cached
units are skipped on rerun) and **deterministic** (two warm-cache runs
produce byte-for-byte identical output). Cache keys cover the model and the
prompt templates, so editing a prompt or switching models re-runs only what
it affects. Token usage is tracked per phase
and shown live.

## Quick start

Requirements: Go ≥ 1.27, a C toolchain (CGo for tree-sitter), an
OpenAI-compatible endpoint.

```sh
make build

export LLM_BASE_URL=https://api.deepseek.com/v1   # or your vLLM, or any provider
export LLM_API_KEY=sk-...                          # empty accepted (local vLLM)
export LLM_MODEL=deepseek-chat

bin/codetospec run --src /path/to/legacy-repo --out /path/to/spec-graph
```

On an interactive terminal, `run` shows a full-screen dashboard (files
parsed, facts, map/reduce progress, failures, live token counts; `q` stops
cleanly and the run resumes later). Use `--no-tui` for plain logs (CI).
An output directory belongs to one source: `run` refuses an `--out` whose
`.codetospec/state.json` records another `--src`, before writing anything.

Other commands:

```sh
bin/codetospec verify --src <repo> --out <graph>   # re-check citations, exit 1 on violation
bin/codetospec drift  --src <repo> --out <graph>   # flag rules whose cited code changed, exit 1 on drift
bin/codetospec stats  --out <graph>                # phase counters; tokens of the last run and all runs
```

Each rule stores a digest of its cited code, so `drift` reports which rules
went stale as the source evolves — the spec stays honest about what it still
matches. When call facts are available (the Go extractor), a rule also
stores the code its cited lines call, one hop (`callees`, `callees_digest`):
a rule whose citation is unchanged but whose helper changed is reported
**affected**, without failing the run, since a callee edit is often
unrelated to the rule. `drift --format json` emits a machine-readable summary and exits 1
on drift, so it drops straight into CI (see `examples/spec-drift.yml`). The
output README also lists near-duplicate rule candidates (a deterministic
consistency check, split into intra- and cross-domain) for a reviewer to
reconcile; two extractor rules are never paired, since they come from one
template and name different operations. It also lists **scattered domains**:
domains whose files fall into several communities of the import graph
(gonum's Louvain, split into connected parts, fixed seed), a sign a domain
lumps unrelated code together (every Go `main` package, a catch-all schema
file). It also lists **shared entities**: entities that rules of several
domains touch, either a shared kernel to name as such or a sign a domain
boundary runs through one concept. A route fact may carry its handler's code span (`handler_path`,
`handler_lines`): any rule citing that span is linked to the endpoint
(`exposed_by`) without the LLM, so the link holds on a warm cache.

`--reduce-mode merge` makes reduce much cheaper: only candidates with a
near-duplicate (similar requirement or overlapping citation) go to the LLM;
the others are kept as mapped, without rationale or acceptance criteria.

Useful flags: `--crosscheck` (adversarial review pass), `--repair` (fix
flagged citations, needs a SCIP index), `--domain-depth N` (split a repo that
lives under one root namespace into `core-controller`, `core-server`… instead
of one mega-domain; a namespace declared in several directories, such as
Go's `main` for every `cmd/<tool>`, is never one domain: each file takes its
directory's, `cmd-seedroster`, and the README's domain map draws those
together in one box), `--reground` (give cached map outputs with ungrounded
rules one correction exchange), `--workers N` (map
parallelism), `--max-tokens N` (raise it if a large domain truncates its
reduce output), `--lang fr|en` (output language: EARS requirements, node labels, README and
llms.txt; default `fr`),
`--exclude 'vendor,node_modules,*.md,*.csv'` (directory names and
file globs), `--facts extra.facts.json` (inject facts from anywhere),
`--ref-root name=dir` (repeatable; also on `verify` and `drift`: an extra
source tree extractor rules may cite, see below).

**Human domain models.** `--domain-model billing.modelith.yaml` (repeatable;
or `domain_models:` in `codetospec.yaml`, relative to `--src`) compares the
graph with a [modelith](https://github.com/stacklok/modelith) model that
people wrote, without an LLM and without changing the model. The model is
linted first: a lint error fails the run before any LLM call. The output
README then gets a **Domain model** section listing:

- model entities with no code entity: matched by name, with case and
  punctuation ignored and a regular plural accepted (`Invoice` matches the
  `invoices` table)
- code entities that no model names
- invariants that no rule states
- rules about modeled entities that state no invariant

Invariants are matched to the rules that touch their entity by word
similarity (Jaccard ≥ 0.3, EARS keywords ignored). A pairing is a lead to
check, not a verdict, and it works only when the model and `--lang` use the
same language. When names differ, map them in the config file:

```yaml
domain_models: [docs/billing.modelith.yaml]
domain_model_entities:
  billing.Ticket: PaperTicket   # model entity → code entity title or node id
```

**Writing models.** `--write-domain-model` (or `write_domain_model: true`)
writes modelith models under `<out>/models/`, never into the source tree:

- each human model, copied at its path relative to `--src`, with what the
  code adds. Human definitions, comments and order are kept. A placeholder
  definition is replaced by the entity's authored description when there is
  one. Columns the entity lacks become attributes. Single-entity ubiquitous
  rules that no invariant states become invariants.
- one `generated/<domain>.modelith.yaml` per domain for the code entities no
  human model names. An entity is defined by its description or, failing
  that, a placeholder; the README lists those placeholders as questions.
- `models/provenance.json`, which says where each field codetospec decided
  comes from (human, description, placeholder, column or rule).

Copy a model back into your repository, edit it, and pass it with
`--domain-model` next time: definitions you write are kept.

## Output

```
<out>/
├── llms.txt          # navigation guide for agents
├── README.md         # summary, coverage report, Mermaid domain graph
├── graph.json        # whole graph, machine format
└── nodes/
    ├── domains/<slug>.md
    ├── entities/<slug>.md
    ├── endpoints/<slug>.md
    └── rules/<domain>.<slug>.md
```

Domain `depends_on` edges are computed from import facts crossing domain
boundaries — never by the LLM. The output README reports coverage: endpoints
referenced by at least one rule, entities touched, failed chunks/domains,
files without a grammar.

Each domain node says whether a missing rule means anything there:
`coverage: complete` when every chunk placed in the domain was mapped and
reduced, `coverage: partial` plus `coverage_gaps` (generated files, failed
map chunks, a failed reduce) otherwise. In a partial domain, "no rule about
refunds" proves nothing; the output README lists the gaps under **Code
without rules**. Excluded paths and files without a grammar belong to no
domain, so they never make one partial.

`llms.txt` indexes every node for an agent; each rule line carries its
citations, crosscheck verdict and grounding, grouped by domain, so an agent
knows which rules to rely on (grounded and supported, repaired or
mechanical) before opening one.

## Extractor protocol (facts v1)

An extractor is any executable configured in `codetospec.yaml` that writes
this to stdout:

```json
{"schema": "codetospec/facts/v1", "facts": [
  {"kind": "route", "id": "route.post./api/activate",
   "attrs": {"method": "POST", "path": "/api/activate", "controller": "..."},
   "source": {"path": "routes/web.php", "lines": "5-5"},
   "origin": "php", "certainty": "static"}
]}
```

```yaml
# codetospec.yaml (at the analyzed repo root)
extractors:
  - name: php
    cmd: php
    args: [extractors/php/extract.php, --root, "{src}"]
    timeout: 300s
```

`kind`: `symbol`, `route`, `table`, `module`, `import`. `certainty`:
`static` or `proved` (runtime-verified). Facts merge by id with priority
`proved` > extractor > tree-sitter, so a runtime-proved route overrides a
statically guessed one. A failing extractor is a warning, not a fatal error.
A `table` fact may carry `description`, the entity's authored description
(a struct's doc comment, on one line); the entity page shows it.

Arguments may use `{src}` (the analyzed root) and `{lang}` (`--lang`).
Two more kinds let an extractor contribute beyond structure (contract in
`internal/extract`):

- `rule` — a business rule derived deterministically from the code. `source`
  is its exact citation; attrs `slug`, `title`, `requirement` (EARS text in
  the output language), `ears`, `nature`, `domain`. Malformed rule facts are
  dropped and counted in the output README. Valid ones become rule nodes
  with `extracted_by: <extractor>` and `crosscheck: mechanical` (never sent to
  the LLM reviewer), are listed to the map prompt as `MECHANICAL_RULES` so the
  model does not restate them, and are grounded like any rule.
  An optional attr `route` (a route fact id) links the rule to that
  endpoint (`exposed_by`). Its `source` may carry `"root": "<name>"`: the
  citation is then in the tree given by `--ref-root <name>=dir`, not
  `--src` — shared code the analyzed one builds on (a monorepo's `pkg/`).
  Such a rule is grounded, digested and verified against that tree, shown
  as `@<name>/path:lines`, and kept as `root:` in the frontmatter; a root
  not given is a `verify` violation and a `drift` "broken" rule. Only
  extractor rules carry a root (chunks come from `--src`).
  A rule fact may also be **inherited** from another spec: attr
  `inherited_from` (that spec's name) with its `crosscheck` verdict,
  `origin` and `confidence`. Such a rule (an LLM rule of shared code) is
  rendered `inherited_from: <spec>` — never `extracted_by` — and is not
  reviewed again by `--crosscheck`; it is grounded and digested like any.

**Shared code in a workspace.** A Go workspace whose modules build on a
shared module (districts-jobs and its `pkg/`) gets one spec per module that
includes the rules of the shared code the module uses:

- scan the shared module once, as a spec of its own;
- run the Go extractor on a module with `-shared pkg=<dir>`. It emits a
  `reach` fact for every shared declaration the module reaches: what its
  code names and, in turn, what those declarations name; a reached type
  reaches its methods; interfaces are not followed (a lower bound);
- run codetospec on the module with `--inherit pkg=<shared spec>` and
  `--ref-root pkg=<dir>`. Each rule of the shared spec with one citation in
  reached code is inherited (`inherited_from: pkg`, domain
  `shared-<domain>`, cited `@pkg/…`), keeping its verdict.

`scandistricts.sh pkg`, then `scandistricts.sh <module>` (or `all`), does
this for districts-jobs, into `~/spec-graphs/districts-jobs/<module>`
(`~/spec-graphs/districts` is the webapp districts service's, from
`scanwebapp.sh`).

For Khan webapp, `scanwebapp-authz.sh` lists the authorization rules of every
Go service at once (no LLM): `extractors/webapp -all` on the checkout, the Go
extractor per service, then `scripts/authzall.py` writes `authz.html` (the
authzview page), `authz.csv` and `authz.md` into `~/authz-webapp`.

An endpoint's node id is its verb and path, slugged (`endpoint.safelist-add`);
the path `/` is `root` (`endpoint.root`, `endpoint.get-root`). Routes whose
ids collide (`/login` and `/login/`) share one endpoint node citing both.
- `domain` — attr `domain` assigns the code in `source` to a domain,
  overriding the domain strategy for chunks starting there.
- `call` — attrs `callee_path` and `callee_lines`: the definition at
  `source` calls the one there. A chunk no `domain` fact places takes the
  domain all its callers in its own directory agree on, callers decided
  first (a helper of a helper follows the operation at the top);
  disagreeing callers, no caller or a call cycle leave it be. Callers in
  other directories are ignored: they use a package, whose domain stands.
  The Go extractor emits these.
- `reach` — the shared declaration at `source` (rooted) is reached by the
  scanned code; read by `--inherit`.
- `constant` — attrs `name` (as written in code, e.g.
  `http.StatusBadRequest`) and `value`: grounding then finds a stated `400`
  behind the constant's name.
- `doc` — attr `text`: authored documentation (e.g. a GraphQL field
  description) shown to the map prompt for the chunks overlapping `source`.
- `dependency` — attrs `service` and `operation`: the code at `source`
  calls an operation another service owns. Each service becomes a
  `service` node listing the calls, and calling domains get `depends_on`
  edges to it. Optional `as` (`service-admin` | `user`): the authority the
  call is made with; service-admin calls are marked on the service node.

A `route` fact may carry `handler_path` (the handler's file): its chunks may
then cite the endpoint, without matching handler names to symbols.

## Development

```sh
make check    # lint, test, race, go mod tidy -diff, govulncheck — every module
make corpus   # re-check real rendered graphs against baselines (no LLM)
```

`make check` is what CI runs. The extractors are separate Go modules, so
every target loops over the root and `extractors/{go,scip,webapp}`. Lint
uses the repository's `.golangci.yml`; do not relax it. `make corpus`
re-runs grounding, verify and drift over the graphs listed in
`internal/corpus/testdata/corpus.json` and fails on any count worse than its
baseline; entries absent from the machine, or whose source moved off the
recorded commit, skip. `CODETOSPEC_CORPUS_UPDATE=1 make corpus` re-records
the baselines.

## Limitations (v0.1)

- Citations are guaranteed to resolve inside the cited chunk and file — not
  to point at the exact statement.
- Deduplication happens per domain; the same behavior surfaced in two
  domains yields two rules (the output README lists them as cross-domain
  near-duplicate candidates, it does not merge them).
- No agentic reduce, no SQLite, no MCP server. See
  [docs/05-limits.md](docs/05-limits.md) for what the method itself cannot
  guarantee.

## License

MIT
