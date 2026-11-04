# Which map-prompt change drops technical rules — 2026-11-04

## Question

`remap-rule-drop-2026-11-04.md` found pkg's map giving 1,477 → 1,212
candidates after the remap, technical 786 → 550, with the code and the
model unchanged: the prompt is the cause. Between `c9b009e` and `9a18cc5`
the map prompt gained EARS components (`7324aae`), a structured condition
with a vocabulary (`d6b0ff3`) and actors; its extraction guidance ("Focus
on business behavior … Ignore framework plumbing", "If the chunk contains
no business rule, return []") did not change. Which addition cuts the
technical rules, and what restores them?

## Revisions

- codetospec `b1b1192` plus this change; old prompt from `c9b009e`.
- webapp `88b55ce5c33` (`git show` of each chunk's file: chunk ids hash
  the content, the same 764 chunks in both runs).
- Model `gemini-3.8-flash`, temperature 0.1, `max_tokens` 16384, as
  `scanwebapp.sh` sends them.

## Command

First, disposable Python over both runs' map caches
(`~/spec-graphs{.before-remap-2026-11-02,}/pkg/.codetospec/map`):
candidates by nature and chunk size.

Then a disposable Python harness (stdlib, OpenAI-compatible endpoint)
replaying the map's first reply (one correction on invalid JSON, as
`llm.ChatJSON`; no other validation, no reground) on 60 pkg Go chunks
(seed 1): 40 of ≥ 100 lines that gave technical rules in the old run, 20
of < 100 lines that gave rules. Each chunk 4 times per variant:

| variant | requirement | condition + VOCABULARY | extraction guidance |
|---|---|---|---|
| old | sentence (`c9b009e`) | no | as before |
| comp | components | no | as before |
| cond | sentence | yes | as before |
| new | components | yes | as before (shipped at `9a18cc5`) |
| fix | components | yes | + "a rule needs no condition" after the condition bullet |
| fix2 | components | yes | fix + "technical behavior … is a rule too … never leave it out; ignore only plumbing that does nothing observable" |
| fix3 | components | yes | "Technical behavior a caller or operator can observe (error mapping, logging, headers, retries) is a rule too, with nature "technical"." before "Ignore framework plumbing." |

Every variant had empty ALLOWED_ENTITIES, ENDPOINTS, ACTORS and
VOCABULARY, and no PRECISE_SYMBOLS, MECHANICAL_RULES or SCHEMA_DOCS
sections. The shipped prompt was dumped from `systemText("en")` and
checked byte-equal to fix3.

## Result

The recorded runs, by chunk size (chunks, technical old → new, all old
→ new):

| lines | chunks | technical | all |
|---|---:|---|---|
| < 40 | 183 | 112 → 89 | 210 → 184 |
| 40–99 | 155 | 239 → 174 | 377 → 316 |
| ≥ 100 | 426 | 435 → 287 | 890 → 712 |

The ablation, mean rules per reply (sd: per-chunk standard deviation of
the rule count over the 4 replies, averaged):

| variant | big: technical | business | all | sd | small: technical | all | completion tokens (big) |
|---|---:|---:|---:|---:|---:|---:|---:|
| old | 2.24 | 1.32 | 3.58 | 0.52 | 0.94 | 1.85 | 465 |
| comp | 1.49 | 1.86 | 3.38 | 0.62 | 0.81 | 1.70 | 572 |
| cond | 1.49 | 1.59 | 3.13 | 0.57 | 0.80 | 1.82 | 514 |
| new | 1.42 | 1.25 | 2.71 | 0.60 | 0.51 | 1.62 | 516 |
| fix | 1.51 | 1.35 | 2.87 | 0.72 | 0.61 | 1.70 | 531 |
| fix2 | 3.13 | 1.72 | 4.88 | 0.82 | 1.39 | 2.52 | 925 |
| fix3 | 2.31 | 1.51 | 3.85 | 0.53 | 1.10 | 2.39 | 736 |

No reply was cut by `max_tokens` (`finish_reason` never `length`); 5
cond, 1 new and 0 fix3 replies stayed invalid JSON after the correction.

Three chunks where new gave no technical rule in its first reply, old's
and fix2's technical titles side by side: the same behaviour comes back
(Stackdriver and Pub/Sub audit events of a script, locale and
service-version headers of the GraphQL client, deterministic ordering of
fragment validators).

## Reading

- **No single change is the cause; each richer field costs technical
  rules.** Components alone or the condition alone each take a third of
  them (2.24 → 1.49). With components the model relabels much of it
  business (all 3.58 → 3.38); with both it drops it (3.58 → 2.71, −24 %,
  the recorded run's −20 % on big chunks).
- **Not truncation, not reground.** No reply hit the cap, completion
  tokens barely move (465 → 516): the model writes fewer rules, not
  longer replies that break. Reground refuses a correction with fewer
  rules, so it cannot drop one.
- **Saying a condition is optional does not help** (fix: 1.51). What
  restores them is naming observable technical behaviour as a rule with
  its nature: fix2 overshoots (4.88 rules, twice the tokens), fix3 is
  back at the old count on big chunks (technical 2.31 vs 2.24, all 3.85
  vs 3.58) with the old run-to-run spread (sd 0.53 vs 0.52). Shipped:
  fix3.
- fix3 overshoots on small chunks (all 2.39 vs 1.85; business 1.29 vs
  0.91): more candidates for the reduce, which merges ~4 % of them.

## Limits

- One output (pkg), one model, 60 chunks, first replies only; the
  shipped map also validates EARS, conditions and labels, and regrounds.
- Empty allowed lists and no context sections: the real prompt is
  longer and names entities; every variant shares that difference.
- Counts, not quality: 3 chunks read by title, no crosscheck of the
  recovered rules.
- The 4 replies per variant measure run-to-run variance on these chunks
  only; a full remap's variance is not measured (`TODO.md`).
