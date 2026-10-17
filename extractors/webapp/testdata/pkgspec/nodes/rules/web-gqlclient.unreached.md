---
id: rule.web-gqlclient.unreached
type: rule
status: generated
sources:
  - path: web/gqlclient/client.go
    lines: "34-34"
edges:
  - {type: belongs_to, to: domain.web-serve}
ears: unwanted
nature: business
origin: explicit
confidence: 0.90
crosscheck: supported
---

# unreached

**Requirement (EARS)**: The system shall return 1.

**Sources**: `x`
