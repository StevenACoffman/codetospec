---
id: NFR-001
title: Signed webhooks
type: NFR
quality_attribute: security
codetospec_rule: rule.billing.signed
codetospec_crosscheck: supported
codetospec_threshold: unknown
---

# NFR-001: Signed webhooks

## Statement

The system shall accept only signed webhooks.

## Measurement and Evaluation

| Metric | Target | Threshold | Method |
|--------|--------|-----------|--------|
| Signed webhooks | Unknown: neither the requirement nor the cited code names a quantity | Unknown: neither the requirement nor the cited code names a quantity | To be decided with the target |

## Verification

Not verifiable until a person sets a target and threshold in targets.yaml.

## Acceptance Criteria

| ID | Criteria | Verification |
|----|----------|--------------|
| NFR-001-AC-1 | An unsigned webhook is rejected with 401. | Test |

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
