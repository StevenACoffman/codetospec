---
id: FR-002
title: Round invoices
type: FR
quality_attribute: reliability
codetospec_rule: rule.billing.round
codetospec_crosscheck: supported
---

# FR-002: Round invoices

## Description

The system shall round to the cent.

## Acceptance Criteria

| ID | Criteria | Verification |
|----|----------|--------------|
| FR-002-AC-1 | 1.005 \| rounds to 1.01. | Test |
| FR-002-AC-2 | 1.004 rounds to 1.00. | Test |

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
- **Tests**: `billing_test.go#TestRound`, `billing_test.go#TestTotal`
