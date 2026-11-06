# Condition disagreements once check_args is read

Question: the 2026-11-04 measurement found extractor rules flagged as
disagreeing with their condition only because the EARS text names a
check's scope or a rate limit's numbers that the condition notation can't
state. Once the extractor carries those values in `check_args` and
grounding counts them as stated (`policy_inventory_plan.md` Part 2,
design 3), how many disagreements and `condition_mismatch` questions go?

## Revisions and commands

- codetospec working tree on `ccc0566` with FIX.md R1-R3 and the policy
  plan's Part 1 and Part 2 steps 1-2 (`checks`, `check_args`,
  `access_check`, `unguarded`, cross-type delegations) and the grounding
  change; webapp `c55b04b0090`.
- Before: `~/spec-graphs/{districts,users}` from the 2026-11-05 remap.
- After: their copies rerun with the remap's flags (`scanwebapp.sh <svc>
  --quality --crosscheck --write-domain-model`, `PKG_GRAPH=~/spec-graphs/pkg`).

## Results

| | districts before | after | users before | after |
|---|---:|---:|---:|---:|
| extractor rules | 438 | 457 | 316 | 316 |
| with `check_args` | 0 | 242 | 0 | 223 |
| with `condition_disagreement` | 164 | 98 | 170 | 87 |
| of which name a scope | 0 | 0 | 8 | 0 |
| of which name a number (rate limits) | 2 | 2 | 41 | 1 |
| `condition_mismatch` questions (all rules) | 441 | 375 | 381 | 298 |

- 149 of 334 extractor-rule disagreements go across the two services, and
  149 questions with them.
- Every disagreement left on districts is the text quoting a whole call
  that the condition names as a variable (`` `pubsub.DecodeAndValidatePushMsg(ctx, r)` ``
  against `pubsub.DecodeAndValidatePushMsg.fails`): the other half of the
  `TODO.md` item "Narrow `condition_mismatch`", not this change's.
- districts' 19 more extractor rules are the cross-type delegations
  (`docs/verification/policy-checks-2026-11-06.md`).

### Cost of the rerun

- users: no LLM call.
- districts: map 26,463, reduce 16,755, crosscheck 11,613 and quality 2,119
  tokens. The map shows each chunk the extractor rules in it
  (`MECHANICAL_RULES`), so the chunks holding the new delegation rules are
  mapped again, and their domains reduced and their rules crosschecked
  again. A one-time cost of adding extractor rules, not of `check_args`.

## Limits

- Two services. Rate limits are mostly users'; scopes mostly admin's and
  users'.
