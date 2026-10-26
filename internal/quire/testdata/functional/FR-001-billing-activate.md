---
id: FR-001
title: Activate
type: FR
codetospec_rule: rule.billing.activate
codetospec_crosscheck: unchecked
codetospec_criteria: requirement
---

# FR-001: Activate

## Description

WHEN a plan starts, the system shall bill it.

## Acceptance Criteria

| ID | Criteria | Verification |
|----|----------|--------------|
| FR-001-AC-1 | WHEN a plan starts, the system shall bill it. | Test |

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
