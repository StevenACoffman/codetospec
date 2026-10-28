# Requirements Document

## Introduction

Bills subscribers.

Recovered by codetospec from the code of the `billing` domain: each requirement states behavior the cited code implements.

## Glossary

- **System**: the code of the `billing` domain.
- **scheduler**: Runs the nightly jobs.

## Requirements

### Requirement 1: Rule legacy

<!-- codetospec: rule.billing.legacy · crosscheck: partial · code: app/billing.go:10-20 -->

#### Acceptance Criteria

1. Once paid, the system shall ship

### Requirement 2: Rule nightly

<!-- codetospec: rule.billing.nightly · crosscheck: unchecked · code: app/billing.go:10-20 -->

#### Acceptance Criteria

1. THE scheduler SHALL close the invoices at midnight

### Requirement 3: Rule prorata

<!-- codetospec: rule.billing.prorata · crosscheck: supported · code: app/billing.go:10-20 -->

#### Acceptance Criteria

1. WHILE the plan is active, WHEN a subscriber joins mid-month, THE SYSTEM SHALL bill the remaining days
