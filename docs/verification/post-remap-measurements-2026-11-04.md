# Post-remap measurements — 2026-11-04

## Question

TODO.md deferred five measurements until every corpus graph had been
remapped with `--quality --crosscheck --acceptance`:

1. how many questions a narrowed `nature_mismatch` would raise, per rule
   and per layer;
2. how many NFRs still raise `nfr_target`, and how many have their value in
   config;
3. how often webapp map conditions reuse the extractor's condition
   variables;
4. the volume and precision of `condition_mismatch`;
5. whether per-layer disagreements or enum transitions give structural
   proposals worth a file.

## Revisions

- codetospec `a842d68` (the remap ran on `9a18cc5`; `a842d68` adds only
  `DOMAIN_PLAN.md`, `scripts/domainsplit.py` and a doc).
- webapp `88b55ce5c33`, districts-jobs at its checkout of 2026-11-03.
- Model `gemini-3.8-flash`, 4 workers. Outputs: `~/spec-graphs` (8 webapp
  graphs, 21 districts-jobs modules), written 2026-11-02 to 2026-11-04.

## Command

Read only, on the outputs.

- (1) Two disposable Go tests (deleted after the run) loading
  `~/spec-graphs/districts` with `render.LoadDir` and mapping A of
  `positional-nature-2026-11-01.md` through `position.Parse`: one counted,
  the other listed each placed rule's positional and text nature. That
  list was joined with
  `uv run scripts/domainsplit.py ~/spec-graphs/districts
  ~/khan/webapp/services/districts <dir>` (DOMAIN_PLAN.md recommendation
  4), using its location-free verdict `verdict_nl`.
- (2)–(5) Disposable Python over `questions.json`, `graph.json` and the rule
  frontmatter (the block between the first two `---` lines).

## Result

### 1. `nature_mismatch` on districts, mapping A

| | rules |
|---|---:|
| placed by the mapping | 1,025 |
| disagree (one question each today) | 226 (22 %) |
| would change the quality pass (presentation on one side) | 34 |
| would change the export (`quality.IsNFR` differs, rule not refuted) | 47 |
| either: the narrowed per-rule question | 81 (8 %) |

| layer | placed | disagree | rate | outcome-changing |
|---|---:|---:|---:|---:|
| `cmd` | 128 | 113 | 88 % | 20 |
| `handlers` | 35 | 33 | 94 % | 13 |
| `models` | 226 | 59 | 26 % | 37 |
| `permissions` | 43 | 0 | 0 % | 0 |
| `rostering` | 593 | 21 | 4 % | 11 |

Before the remap (same mapping, older graph): 1,180 placed, 254
disagreeing.

Which side the plan's evidence takes on the 226 (subject → business):

| layer | with the text | with the position | undecided | neither |
|---|---:|---:|---:|---:|
| `cmd` | 105 | 0 | 6 | 2 |
| `handlers` | 23 | 10 | 0 | 0 |
| `models` | 0 | 30 | 29 | 0 |
| `rostering` | 0 | 16 | 5 | 0 |
| total | 128 | 56 | 40 | 2 |

`cmd/` is mostly one-off commands and backfills (`dist-4399-backfill`,
`seedpartnerships`, `send-kad-emails`). Of the 128 that side with the text,
61 rest on business words in the requirement alone ("district" is in most
districts rules); 8 of the 128, read by hand, are business logic in 7
cases (seeding, tree validation, Khanmigo enablement). `domainsplit.py`
has no presentation class: the 31 rules that are presentation by text
and business by position count as "with the position" (14) or undecided
(17), and say nothing about presentation.

No output carries `nature_by_position`: no `codetospec.yaml` of webapp or
districts-jobs sets `nature_by_path` or `nature_by_layer`.

### 2. `nfr_target`

2,280 questions over all outputs, 1,229 distinct rules (an inherited pkg
rule asks once per service that inherits it). By attribute:
maintainability 680, reliability 466, security 404, performance 366,
compatibility 183, other 181.

24 questions sampled at random (23 distinct rules), read by hand: none
states a quantity a target could fill, so none has one in config. They
are technical behaviour classified as a quality: "delete the entry's
key", "set isKeysOnly", "log a warning". Three read a configured name (a
default bucket name, `KA_SERVICE_NAME`, a default model), none a
threshold.

175 of the 1,229 carry a measurable word (timeout, limit, TTL, any digit).
In 12 of them, sampled, the quantity lives in code, never in config: a
package constant (`_defaultCacheKeyTTLJitterFrac = 0.036`), a package
variable (`PerThreadSnapshotLimit = 128`), or a field its caller sets
(`RefreshEvery` of the settings cache).

### 3. Webapp map conditions and extractor variables

| | rules |
|---|---:|
| LLM rules with a condition (7 webapp outputs) | 6,531 |
| naming an extractor condition variable | 74 (1.1 %) |
| hashing equal to an extractor rule's condition | 5 |
| naming any vocabulary variable (not `local.`) | 2,258 (35 %) |
| all variables coined under `local.` | 4,273 (65 %) |

Vocabulary variables by prefix: `models` 1,066, `cross_service` 114,
`main` 110, `config` 101, then struct names. The map prompt lists the
extractor's rules as `MECHANICAL_RULES` with "do NOT restate them"
(`internal/mapper/mapper.go:476`).

### 4. `condition_mismatch`

| rule origin | with a condition | disagreement | side |
|---|---:|---:|---|
| extractor (webapp) | 968 | 788 (81 %) | all text |
| inherited (counted per service) | 3,419 | 421 | 392 condition, 29 text |
| LLM | 8,851 | 924 (10 %) | 843 condition, 69 text, 12 both |

Extractor rules. The template quotes the guard in backticks and the
condition carries it in a variable path
(`caller.hasPermission.CanViewDistrictRecords`), never as a value. 490 of
the 788 flag only such names (556 calls and 331 identifiers in all). The
other 298 also flag what the condition leaves out: the scope or argument
of a check (269 values: `user.Kaid`, `acl.GlobalScope`, `actor == nil`)
and a rate limit's numbers (79: `200 requests per 600 seconds`). Both are
the same for every rule of a template.

LLM rules, condition side. The values: identifiers 773, words and strings
258, numbers 194, `nil` 114. 20 disagreements sampled at random, read by
hand: the 19 condition-side ones are all prose paraphrasing a value (an
enum constant `RosterSourceTeacherDirected` ↔ "teacher-directed", `nil` ↔
"configured", `1` ↔ "exactly one" or "multiple", `"--"` ↔ "double dash",
`"[]byte"` ↔ "byte slice"). A condition's values are already grounded in
the cited code (`grounding.Annotate` drops it otherwise), so this side
only says the text is vaguer than the code.

LLM rules, text side. 15 sampled (12 LLM, 3 inherited) plus the 1 of the
first sample: 3 of 13 LLM rules useful, each a condition that omits a
check ("under 18", a 5–6 day window, a lowercase key the condition does
not test). The false ones: a value carried by a variable's name
(`local.isDefinitelyUnder18`, `local.twelveHours`,
`local.expiryPlus90Days`), a named constant (`500` ↔
`http.StatusInternalServerError`), a bit width ("64-bit", "32-bit"), a
quoted identifier the condition names.

### 5. Structural proposals

Enum transitions over every output: 10 (8 enums, in ai-guide and
districts).

## Reading

- **`nature_mismatch`.** Narrowing per rule cuts 226 questions to 81; a
  per-layer threshold of 25 % would ask about 3 layers, 50 % about 2.
  Against the plan's evidence, the two high layers are mapping errors:
  text and evidence agree against the position in 128 of 226, almost all
  in `cmd/` and `handlers/`. That half of the evidence is weak (business
  words alone), but reading agrees with it, as on 2026-11-01. The
  conflicts left are in `models/` and `rostering/` (56 with the position,
  34 undecided). The feature is off on every output today.
- **`nfr_target` is not short of config.** Most questions ask a target of
  a rule that has none to give: technical behaviour, not a measured
  quality. Where a quantity exists, it is in code.
- **Webapp map conditions do not restate the extractor's checks,** as
  their prompt asks, so 1.1 % reuse is expected. A third of map
  conditions name a vocabulary variable, mostly struct fields.
- **`condition_mismatch` precision is low on every side.** Extractor
  rules: none is useful as a per-rule question; 298 show what the
  extractor's condition notation leaves out, a fix for the extractor. LLM
  rules: the condition side is paraphrase (0 of 19); the text side is
  useful about 1 time in 4, and most false ones carry the value in a
  variable's name or a named constant.
- **Neither source gives structural proposals worth a file.**

## Limits

- One judge (the author, not a domain expert); samples of 8–24.
- `nature_mismatch`: one hand-written mapping on one service; no Khan
  service has an anarch.json for `nature_by_layer`. `domainsplit.py` is
  judged by its own author, uses a hand-made stoplist, and reads the
  frontmatter up to the first `---` anywhere: 18 of 20,246 rules
  (`'---'` in a value) lose the rest of it.
- Inherited rules are counted once per inheriting service.
- The measurable-word regex is broad (any digit counts).
