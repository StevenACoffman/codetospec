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

**Values**:

- `Draft` = `draft`
- `Paid` = `paid`

**Transitions** (code that, while the value is From, sets it to To):

| From | To | Code |
|------|----|------|
| `Draft` | `Paid` | `app/Models/Invoice.php:20-24` |

**Sources**: `app/Models/Invoice.php:5-5` · `app/Models/Invoice.php:20-24`
