# Tests shown to the crosscheck — 2026-11-01

## Question

The crosscheck reviewer now reads, beside a rule's cited code, the code of
the two nearest tests reaching it (`testreach.NearestTests`, a new `TESTS`
section of the prompt). Does that keep the reviewer's accuracy on the
planted faults, and does a test that does itself what a rule credits the
code with (a mock) mislead it?

## Revisions

- codetospec: `81a5f55` plus the uncommitted change of the same day
  (prompt: `TESTS` section and the rule "do NOT credit behavior that
  appears only in a test"; the tests excerpt is folded into the cache key).
- Fixture: `testdata/fixture/tests/Unit/ProrataCalculatorTest.php`, new:
  `testActivationMidMonthIsProrated` asserts `calculate(30.0, 16, 30)` is
  `15.0`; `testActivationSendsReceipt` mocks a mailer and sends a receipt
  itself after calling `calculate`.
- Model: `gemini-3.8-flash`.

## Commands

```sh
# before the prompt change (8 cases)
CODETOSPEC_EVAL=1 CODETOSPEC_EVAL_REPEAT=3 CODETOSPEC_EVAL_RECORD=eval-before.json \
  go test ./internal/crosscheck -run Eval -v
# after (the 8 cases, plus 2 that carry a test)
CODETOSPEC_EVAL=1 CODETOSPEC_EVAL_REPEAT=3 CODETOSPEC_EVAL_RECORD=eval-after.json \
  CODETOSPEC_EVAL_BASELINE=eval-before.json go test ./internal/crosscheck -run Eval -v
```

(with `LLM_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai`,
`LLM_MODEL=gemini-3.8-flash`, `LLM_API_KEY`.)

## Result

| | faults caught | false alarms | flipped | balanced accuracy |
|---|---:|---:|---:|---:|
| before, 8 cases × 3 | 12/12 | 0/12 | 0 | 1.00 |
| after, 10 cases × 3 | 15/15 | 0/15 | 0 | 1.00 |

- The 8 shared cases: the same verdict in every run before and after.
- `control-prorata-tested` (a control whose test confirms its values):
  supported 3/3.
- `test-only-receipt` (a planted rule crediting the code with the receipt
  the test sends itself): unsupported 3/3, each reason saying the receipt
  appears only in the test.

Meets the adoption bar of AGENTS.md (balanced accuracy no lower, no more
false alarms, no more flips).

## Limits

- Ten cases on one small fixture, one model; the eval was already at
  ceiling before, so it shows no regression, not a gain.
- What the tests add on real rules (fewer `partial` verdicts on rules whose
  values a test pins?) is unmeasured: it needs a `--crosscheck` run on a
  graph with Go test facts.
