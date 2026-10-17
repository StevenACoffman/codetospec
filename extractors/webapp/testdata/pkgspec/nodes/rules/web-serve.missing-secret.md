---
id: rule.web-serve.missing-secret
type: rule
status: generated
sources:
  - path: web/serve/graphql.go
    lines: "15-18"
edges:
  - {type: belongs_to, to: domain.web-serve}
ears: unwanted
nature: business
origin: explicit
confidence: 0.90
crosscheck: supported
---

# missing-secret

**Requirement (EARS)**: IF the X-Ka-Gateway-Secret header is empty, THEN the system shall answer 401.

**Sources**: `x`
