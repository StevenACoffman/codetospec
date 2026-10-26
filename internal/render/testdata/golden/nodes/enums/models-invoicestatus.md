---
id: enum.models-invoicestatus
type: enum
status: generated
sources:
  - path: app/Models/Invoice.php
    lines: "5-5"
  - path: app/Models/Invoice.php
    lines: "20-24"
edges: []
transitions: 1
---

# models.InvoiceStatus

**Valeurs** :

- `Draft` = `draft`
- `Paid` = `paid`

**Transitions** (le code, tant que la valeur est « De », la passe à « Vers ») :

| De | Vers | Code |
|----|------|------|
| `Draft` | `Paid` | `app/Models/Invoice.php:20-24` |

**Sources** : `app/Models/Invoice.php:5-5` · `app/Models/Invoice.php:20-24`
