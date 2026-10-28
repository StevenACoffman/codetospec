---
id: rule.billing.prorata-activation
type: rule
status: generated
sources:
  - path: app/Services/Billing/ProrataCalculator.php
    lines: "11-24"
edges:
  - {type: belongs_to, to: domain.billing}
  - {type: touches, to: entity.invoices}
  - {type: exposed_by, to: endpoint.post-api-activate}
ears: event
acceptance: 3
nature: business
origin: explicit
confidence: 0.90
crosscheck: supported
grounding: ungrounded
grounding_missing: "30 @app/Services/Billing/ProrataCalculator.php:9"
ears_issue: "event requirement does not open with WHEN"
tests: 2
test_depth: 1
tested_by: "tests/ProrataTest.php#testActivation, tests/ProrataTest.php#testRefund"
tests_via_interface: 1
executed: 7/9
---

# Prorata on activation

**Requirement (EARS)**: WHEN a subscriber activates mid-month, the system shall bill pro rata for the remaining days.

**Rationale**: Computation seen in ProrataCalculator.

**Acceptance criteria**:
1. Nominal case covered.
2. Edge case covered.
3. Error covered.

**Sources**: `app/Services/Billing/ProrataCalculator.php:11-24`

Links: [Domain billing](../domains/billing.md) · [entity.invoices](../entities/invoices.md) · [endpoint.post-api-activate](../endpoints/post-api-activate.md)
