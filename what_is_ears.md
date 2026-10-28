What is EARS?
-------------

**EARS** (Easy Approach to Requirements Syntax) is a rules-based requirements notation created by Alistair Mavin and his team at **Rolls-Royce PLC**. It was designed to solve a fundamental problem in systems engineering: unconstrained natural language is full of ambiguity, omitted edge cases, and conflicting interpretations, while formal mathematical methods (such as Z or B-Method) require steep training curves that teams resist.

In modern software engineering, EARS has become the standard requirements notation for **Spec-Driven Development (SDD)**. Autonomous AI coding agents like Cursor, Claude Code, and Devin excel at generating code from clear specifications, but they fail when given hand-wavy user stories. EARS provides the exact grammatical constraints LLMs need to generate deterministic, production-ready code on the first run.

> "If you can't write it in EARS, then you don't understand it." \- Systems Engineering Maxim

Global Industry Adoption EARS is utilized in mission-critical systems worldwide by organizations including **NASA, Intel, Siemens, Rolls-Royce, Bosch, Honeywell, Dyson, and Daimler**.

Canonical Clause Ordering & Grammar
-----------------------------------

Every EARS requirement follows a strict, non-negotiable clause sequence. Clauses must always appear in the same order:

While <state>, With <pre-condition>, When <trigger>, the <system name> shall <system response>

### EARS Component Cardinality

Component

Cardinality

Keyword

Description

State-condition

0 or 1

While

A persistent operating mode or state the system is currently in

Pre-condition

0 or many

With / If

Prerequisites that must hold true for the requirement to execute

Trigger

0 or 1

When

The discrete event or user action that initiates the requirement

System name

Exactly 1

the <name>

The explicit subject or service performing the action (never passive voice)

System response

1 or many

shall

The observable action, state mutation, or output the system must execute

The 6 Core EARS Patterns
------------------------

Every software requirement can be categorized into one of six distinct structural patterns:

1\. UBIQUITOUS REQUIREMENTS (Always Active) KEYWORD: None

Used for fundamental, non-negotiable system properties, constraints, or continuous behaviors that execute without triggers or specific states.

Template: The <system name> shall <system response>.

Example: The authentication service shall store all user passwords hashed using Argon2id with a minimum memory cost of 64 MB.

2\. STATE-DRIVEN REQUIREMENTS (Mode Dependent) KEYWORD: While

Used when behavior is constrained to a continuous operating state, environment mode, or status condition.

Template: While <state-condition>, the <system name> shall <system response>.

Example: While the database is in read-only maintenance mode, the API gateway shall reject all mutating HTTP requests (POST, PUT, DELETE, PATCH) with HTTP status 503.

3\. EVENT-DRIVEN REQUIREMENTS (Triggered Reaction) KEYWORD: When

Used for discrete user interactions, inbound messages, or signals that demand an immediate system response.

Template: When <trigger>, the <system name> shall <system response>.

Example: When a developer clicks "Export ZIP", the compiler service shall package all 4 spec markdown files and initiate the browser download stream within 500ms.

4\. OPTIONAL FEATURE REQUIREMENTS (Feature Flags / Variants) KEYWORD: Where

Used for capability tiers, configurable features, hardware variants, or gated flags.

Template: Where <feature is included>, the <system name> shall <system response>.

Example: Where enterprise SAML single sign-on is enabled, the authentication layer shall redirect login requests to the customer's configured identity provider endpoint.

5\. UNWANTED BEHAVIOR REQUIREMENTS (Errors & Faults) KEYWORDS: If ... then

Mandatory for error boundaries, fault recovery, invalid inputs, and security edge cases. Unwanted behavior requirements are the single biggest source of omissions in software specifications.

Template: If <unwanted condition/trigger>, then the <system name> shall <system response>.

Example: If an incoming Stripe webhook signature fails verification, then the webhook handler shall discard the payload, return HTTP status 400, and log an audit security warning.

6\. COMPLEX REQUIREMENTS (Compound Conditions) KEYWORDS: While, With, When, If, then

Used when behavior depends on multiple preconditions, persistent states, and trigger events occurring simultaneously.

Template: While <state-condition>, With <pre-condition>, When <trigger>, the <system name> shall <system response>.

Example: While a user session is active, With uncommitted local spec edits present, When the browser tab receives an unload event, the autosave worker shall write the pending delta to IndexedDB before terminating.

The EARS+ Extension for Agile Teams
-----------------------------------

Standard Agile user stories (_"As a user I want... so that..."_) capture product intent, but they lack technical testability. **EARS+** bridges Agile user stories with EARS syntax by nesting strict EARS patterns directly inside the Acceptance Criteria block.

// EARS+ Story & Criteria Integration

User Story: As an engineering manager, I want to export project specifications to ZIP, so that my team can commit them to our git repository.

Acceptance Criteria:

1.  **WHEN** the user clicks "Export All", **THEN** the export service **SHALL** bundle constitution.md, requirements.md, solution.md, and tasks.md into a single archive.
2.  **IF** any spec file contains unsaved edits, **THEN** the compiler **SHALL** flush pending memory buffers to disk prior to compression.
3.  **WHILE** compression is executing, **THEN** the interface **SHALL** display a progress bar and disable secondary export requests.

### EARS+ Acceptance Criteria Patterns Table

Pattern Name

Syntax Template

Simple Event

WHEN <event> THEN <system> SHALL <response>

Event with Condition

WHEN <event> AND <condition> THEN <system> SHALL <response>

State-based

WHILE <state-condition> THEN <system> SHALL <response>

Error Handling

IF <pre-condition> THEN <system> SHALL <response>

Conditional Event

IF <pre-condition> WHEN <event> AND <condition> THEN <system> SHALL <response>

Full Complex

WHILE <state> WITH <pre-condition> WHEN <event> THEN <system> SHALL <response>

Production requirements.md Specimen
-----------------------------------

Below is the standard structure generated by MySpec for `requirements.md`. It separates functional requirements from non-functional quality gates while assigning unique, traceable identifiers to every requirement.

    # Requirements Document
    
    ## 1. Functional Requirements
    
    ### FR-AUTH-001: User Authentication Flow
    **User Story:** As a developer, I want to authenticate via GitHub OAuth, so that my account is securely linked to my repositories.
    
    **Acceptance Criteria:**
    1. WHEN the user clicks "Sign In with GitHub", THEN the auth service SHALL redirect to the GitHub OAuth authorization endpoint with the required scopes.
    2. WHEN the GitHub callback returns an authorization code, THEN the server SHALL exchange the code for an access token within 2 seconds.
    3. IF the OAuth state parameter is invalid or mismatched, THEN the server SHALL reject the authentication request with HTTP status 403.
    4. WHILE the session token is active, THEN the client SHALL include the bearer token in all downstream API requests.
    
    ### FR-AUTH-002: Token Refresh & Expiration
    **User Story:** As a developer, I want my session refreshed automatically, so that my work is not interrupted by token expiry.
    
    **Acceptance Criteria:**
    1. WHEN an API request returns HTTP 401 with code "token_expired", THEN the client SHALL dispatch a refresh request using the HTTP-only refresh cookie.
    2. IF the refresh token is expired or revoked, THEN the client SHALL redirect the user to /login?session_expired=true.
    
    ## 2. Non-Functional Requirements
    
    ### NFR-PERF-001: Live Preview Latency
    **Acceptance Criteria:**
    1. The markdown parser SHALL render spec previews within 100ms of content mutations (Ubiquitous).
    2. WHILE a Mermaid diagram is compiling, THEN the preview container SHALL display a skeleton loader.
    3. IF the Mermaid syntax is malformed, THEN the editor SHALL display an inline error indicator at the failing line.

EARS Best Practices: Do's and Don'ts
------------------------------------

✓ The Do's

*   **Be specific:** Replace universal quantifiers like "all", "any", or "every" with named domain entities or explicit boundary numbers.
*   **Anticipate failures:** Author dedicated _Unwanted Behavior_ requirements (`If ... then`) for every potential network, auth, or schema failure.
*   **Use list formats for complexity:** When a requirement has 3+ preconditions, format them as an indented list for clarity.
*   **Keep responses atomic:** Ensure each statement has one primary system response rather than packing multiple side effects together.

✗ The Don'ts

*   **Don't use passive voice:** Statements like _"The file should be encrypted"_ omit the subject. Always write _"The compiler shall encrypt the file"_.
*   **Don't scramble clause order:** Never put the trigger (`When`) before the state (`While`). Follow the canonical ordering strictly.
*   **Don't use weak modals:** Never use "should", "could", "might", or "may" in requirements. Always use **shall** for mandatory specifications.
*   **Don't hide assumptions:** If a requirement depends on an external API or database constraint, state it explicitly as a precondition.

### Generate Strict EARS+ Specs Automatically

MySpec conducts an automated architectural interview and formats your messy ideas directly into verified EARS+ acceptance criteria.

[LAUNCH MYSPEC ARCHITECT ↗](https://app.myspec.dev)