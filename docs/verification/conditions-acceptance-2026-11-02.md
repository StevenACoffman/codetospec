# Rule conditions and acceptance facts — 2026-11-02

## Question

IMPROVEMENTS #1 (a structured condition per rule) and #5 (executable
examples), with the acceptance pass that writes #5's examples, land before
the remap. On real code: how many rules get a condition, how many
conditions break or are dropped, how much of the vocabulary the map uses,
how often a condition and its EARS text disagree and why; and for the
acceptance pass, how many observables ground, and whether the examples
hold.

## Revisions

- codetospec: `78ce230` plus the uncommitted conditions and acceptance work
  of the same day.
- districts-jobs: `9a064d61c`, modules `rosterjob-updates` (848 lines of
  Go), `khanxadmin` (897), `moa-transitions` (1,032); and
  `testdata/fixture` (PHP).
- Model: `gemini-3.8-flash`, 4 workers, fresh caches, outputs in a scratch
  directory (`~/spec-graphs` untouched). No `pkg` spec, so no inherited
  rules; no `--crosscheck` on districts-jobs (every LLM rule asked).

## Command

```sh
OUT_ROOT=<scratch>/measure ./scandistricts.sh <module> --acceptance
bin/codetospec run --src testdata/fixture --out <scratch>/fixture \
  --facts testdata/fixture/fixture.facts.json --lang en --crosscheck --acceptance
```

A disposable script counted the rendered rule files' frontmatter. After
the first runs, the acceptance prompt gained one sentence on `exists`
(below); the three modules were rerun on the warm cache, which asked the
acceptance pass alone again.

## Result

| | rosterjob-updates | khanxadmin | moa-transitions | fixture |
|---|---:|---:|---:|---:|
| rules (all LLM) | 30 | 12 | 24 | 6 |
| with a condition | 19 | 9 | 17 | 4 |
| condition variables | 32 | 13 | 24 | 4 |
| of which coined (`local.`) | 32 | 13 | 21 | 2 |
| conditions dropped | 0 | 0 | 0 | 0 |
| condition/text disagreements | 1 | 2 | 2 | 0 |
| observables grounded / unchecked / ungrounded | 13 / 13 / 4 | 7 / 5 / 0 | 7 / 17 / 0 | 5 / 1 / 0 |
| observable from a test / the code | 5 / 25 | 2 / 10 | 14 / 10 | 0 / 6 |
| non-claims | 48 | 19 | 39 | 9 |
| examples held / written | 48 / 48 | 25 / 25 | 43 / 43 | 11 / 11 |

Tokens of the first runs (prompt+completion): map 88k+7k, 58k+3k, 48k+4k;
acceptance 24k+4k, 8k+1k, 60k+3k (moa-transitions shows 14 tests).

**Conditions.** 45 of 66 districts-jobs rules got one; none failed to
parse or ground after the map's repair exchange. The vocabulary had
almost nothing to offer these modules: no tables, no domain model, and
the Go extractor states no rule conditions. 66 of 69 variables were
coined under `local.`; the 3 others are `moa.State`, an enumeration the
Go extractor reports, used as offered. So on these modules conditions
cannot match across rules yet: the vocabulary is the gap, not the
notation.

**Disagreements.** 5, read by hand:

- `text 180 (cited)`: the condition (`exists local.expiration`) omits the
  180-day window the text and the code state. The condition is wrong.
- `condition 1`, `condition ":"`: the text states the value in words
  ("the single argument") or not at all (a key "that requires
  rewriting"); the condition is the precise one.
- `condition "COMPLETED_SUCCESSFUL"; "COMPLETED_UNSUCCESSFUL"`,
  `condition "StateExpired"; "StateSuspended"`: the text puts the check
  in the response ("shall return true if the status is …") or names the
  states in words; the values occur outside the condition clauses.

One of five points at a wrong condition; the others at text vaguer than
its code. Both are worth a reviewer's look, which is what a question is;
a "zero is implicit" rule (0 and "" are not required in the text) was
added after the fixture flagged `amount < 0` against "negative".

**Examples.** Before the prompt sentence, 3 examples were contradicted
(rosterjob-updates 1, khanxadmin 2): each gave an `exists` variable the
value `false` and expected the check to fail, but `exists` holds for any
given value. The prompt now says so; after it, 116 of 116 examples hold
on districts-jobs and 11 of 11 on the fixture. No contradicted example
was a wrong rule.

**Bugs found by these runs, fixed before the counts above:** the
acceptance cache read numbers back as floats, so a cached `2.0` rendered
as `2` (a warm run differing from the live one: `TestRun` now pins
equality); go-rule-engine refuses to compare an int with a float, so an
example `0` against a condition `0.0` was undecidable — every number is
now a float, 3 and 3.0 one value.

## Decision

Keep conditions and the acceptance pass as built. Disagreements stay
questions, neither side overridden. Grow the vocabulary before relying on
cross-rule matching: on Go services without tables or models, the
extractor's types (struct fields, enums) are the source left to add.

## Limits

- Three small modules and a fixture; one model.
- No webapp run: its extractor conditions (34 in the goldens), the
  vocabulary's richest source, were not measured against map conditions.
- Observable grounding is mostly `unchecked` (35 of 66): many checks state
  no number or quoted string to look for.
- Disagreement detection compares values only; structure (`and` versus
  `or`, a lost negation) is not compared.
