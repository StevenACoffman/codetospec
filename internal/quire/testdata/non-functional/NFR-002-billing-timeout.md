---
id: NFR-002
title: Payment call timeout
type: NFR
quality_attribute: performance_efficiency
codetospec_rule: rule.billing.timeout
codetospec_crosscheck: supported
codetospec_threshold: stated
---

# NFR-002: Payment call timeout

## Statement

The system shall time out a payment call after 30 seconds.

## Measurement and Evaluation

| Metric | Target | Threshold | Method |
|--------|--------|-----------|--------|
| Payment call timeout | 30 seconds (stated in the code) | 30 seconds (stated in the code) | Inspection of the cited code |

## Verification

Inspection: the cited code sets the threshold, and codetospec's grounding check found each stated value in it.

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
