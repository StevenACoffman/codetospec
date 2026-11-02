---
id: NFR-001
title: with an audit trail
type: NFR
quality_attribute: performance_efficiency
codetospec_rule: rule.billing.audit
codetospec_crosscheck: supported
codetospec_threshold: set
codetospec_target: policy
---

# NFR-001: with an audit trail

## Statement

The system shall answer with an audit trail.

## Measurement and Evaluation

| Metric | Target | Threshold | Method |
|--------|--------|-----------|--------|
| with an audit trail | Compliance with the policy (NFR-001-AC-1) | Compliance with the policy (NFR-001-AC-1) | Review against the policy |

## Verification

Inspection: a reviewer checks the code against the policy set in targets.yaml (the Acceptance Criteria below).

## Acceptance Criteria

| ID | Criteria | Verification |
|----|----------|--------------|
| NFR-001-AC-1 | Every change to an invoice is logged with its author. | Inspection |

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
