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

# Domaine billing

Facturation des abonnés.

## Règles

- [Prorata à l'activation](../rules/billing.prorata-activation.md)

Liens : [domain.crm](../domains/crm.md)
