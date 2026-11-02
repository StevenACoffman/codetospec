---
id: FR-001
title: Activate
type: FR
codetospec_rule: rule.billing.activate
codetospec_crosscheck: unchecked
codetospec_criteria: requirement
codetospec_observable: FR-001-AC-2
codetospec_non_claims:
    - does not charge the card
    - does not email the receipt
---

# FR-001: Activate

## Description

WHEN a plan starts, the system shall bill it.

## Acceptance Criteria

| ID | Criteria | Verification |
|----|----------|--------------|
| FR-001-AC-1 | WHEN a plan starts, the system shall bill it. | Test |
| FR-001-AC-2 | The response holds the first invoice. | Test |

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
