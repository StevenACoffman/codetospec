---
id: rule.web-serve.two-files
type: rule
status: generated
sources:
  - path: web/serve/graphql.go
    lines: "15-18"
  - path: web/gqlclient/client.go
    lines: "27-29"
edges:
  - {type: belongs_to, to: domain.web-serve}
ears: unwanted
nature: business
origin: explicit
confidence: 0.90
crosscheck: supported
---

# two-files

**Requirement (EARS)**: The system shall check twice.

**Sources**: `x`
