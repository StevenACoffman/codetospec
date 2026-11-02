---
id: NFR-001
title: Signed webhooks
type: NFR
quality_attribute: security
codetospec_rule: rule.billing.signed
codetospec_crosscheck: supported
codetospec_threshold: unknown
codetospec_question: q-d02f3ccb9ce1
---

# NFR-001: Signed webhooks

## Statement

The system shall accept only signed webhooks.

## Measurement and Evaluation

| Metric | Target | Threshold | Method |
|--------|--------|-----------|--------|
| Signed webhooks | Unknown: the code states none (question q-d02f3ccb9ce1) | Unknown: the code states none (question q-d02f3ccb9ce1) | To be decided with the target |

## Verification

Not verifiable until a person sets the target and threshold: question q-d02f3ccb9ce1 in questions.json.

## Acceptance Criteria

| ID | Criteria | Verification |
|----|----------|--------------|
| NFR-001-AC-1 | An unsigned webhook is rejected with 401. | Test |

## Dependencies

- **Domain**: `billing`
- **Code**: `app/Billing.php:10-20`
