---
id: NFR-003
title: within 30 seconds
type: NFR
quality_attribute: performance_efficiency
codetospec_rule: rule.billing.timeout
codetospec_crosscheck: supported
codetospec_threshold: stated
---

# NFR-003: within 30 seconds

## Statement

The system shall answer within 30 seconds.

## Measurement and Evaluation

| Metric | Target | Threshold | Method |
|--------|--------|-----------|--------|
| within 30 seconds | 30 (stated in the code) | 30 (stated in the code) | Inspection of the cited code |

## Verification

Inspection: the cited code sets the threshold, and codetospec's grounding check found each stated value in it.

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
