# Requirements Document

## Introduction

Recovered by codetospec from the code of the `crm` domain: each requirement states behavior the cited code implements.

## Glossary

- **System**: the code of the `crm` domain.
- **notifier**: Named by these requirements; the glossary does not describe it.

## Requirements

### Requirement 1: Rule retry

<!-- codetospec: rule.crm.retry · crosscheck: supported · code: app/crm.go:10-20 -->

#### Acceptance Criteria

1. WHEN a sync fails, IF it failed three times, THEN THE notifier SHALL page the owner
