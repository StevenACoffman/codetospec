---
id: rule.billing.prorata-activation
type: rule
status: generated
sources:
  - path: app/Services/Billing/ProrataCalculator.php
    lines: "11-24"
edges:
  - {type: belongs_to, to: domain.billing}
  - {type: touches, to: entity.invoices}
  - {type: exposed_by, to: endpoint.post-api-activate}
ears: event
acceptance: 3
nature: business
origin: explicit
confidence: 0.90
crosscheck: supported
grounding: ungrounded
grounding_missing: "30 @app/Services/Billing/ProrataCalculator.php:9"
ears_issue: "event requirement does not open with WHEN"
tests: 2
test_depth: 1
tested_by: "tests/ProrataTest.php#testActivation, tests/ProrataTest.php#testRefund"
tests_via_interface: 1
executed: 7/9
---

# Prorata à l'activation

**Exigence (EARS)** : QUAND un abonné active en cours de mois, le systeme doit facturer au prorata des jours restants.

**Justification** : Calcul observé dans ProrataCalculator.

**Critères d'acceptation** :
1. Cas nominal couvert.
2. Cas limite couvert.
3. Erreur couverte.

**Sources** : `app/Services/Billing/ProrataCalculator.php:11-24`

Liens : [Domaine billing](../domains/billing.md) · [entity.invoices](../entities/invoices.md) · [endpoint.post-api-activate](../endpoints/post-api-activate.md)
