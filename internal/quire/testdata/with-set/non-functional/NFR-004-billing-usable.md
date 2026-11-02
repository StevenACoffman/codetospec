---
id: NFR-004
title: plainly
type: NFR
quality_attribute: performance_efficiency
codetospec_rule: rule.billing.usable
codetospec_crosscheck: supported
codetospec_threshold: set
codetospec_target: example
---

# NFR-004: plainly

## Statement

The system shall answer plainly.

## Measurement and Evaluation

| Metric | Target | Threshold | Method |
|--------|--------|-----------|--------|
| plainly | As the reference case: the checkout of the 2026 redesign | No worse than the reference case | Comparison with the reference case |

## Verification

Demonstration: the system is compared with the reference case set in targets.yaml.

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
