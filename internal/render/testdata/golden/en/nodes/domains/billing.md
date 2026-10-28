---
id: domain.billing
type: domain
status: generated
sources: []
edges:
  - {type: depends_on, to: domain.crm}
coverage: partial
coverage_gaps: "map-failed app/Services/Billing/Refunds.php:1-40"
---

# Domain billing

Subscriber billing.

## Rules

- [Prorata on activation](../rules/billing.prorata-activation.md)

Links: [domain.crm](../domains/crm.md)
