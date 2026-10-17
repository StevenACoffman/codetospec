---
id: rule.web-gqlclient.send-needs-headers
type: rule
status: generated
sources:
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

# send-needs-headers

**Requirement (EARS)**: IF the sender has no headers, THEN the system shall reject the send.

**Sources**: `x`
