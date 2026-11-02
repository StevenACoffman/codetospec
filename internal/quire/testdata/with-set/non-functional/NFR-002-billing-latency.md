---
id: NFR-002
title: quickly
type: NFR
quality_attribute: performance_efficiency
codetospec_rule: rule.billing.latency
codetospec_crosscheck: supported
codetospec_threshold: set
codetospec_target: measure
codetospec_question: q-16a3230310b0
---

# NFR-002: quickly

## Statement

The system shall answer quickly.

## Measurement and Evaluation

| Metric | Target | Threshold | Method |
|--------|--------|-----------|--------|
| p95 latency of POST /invoices | 120 ms | 200 ms | load test, 10 minutes |

## Verification

Measurement: load test, 10 minutes, against the target set in targets.yaml.

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
