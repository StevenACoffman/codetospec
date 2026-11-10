# D2 diagrams and the access views, 2026-11-10

Diagrams moved from Mermaid to D2 (decided 2026-11-10): the output
README's domain map, and the new access views (`export --format views`).
SVG comes from `codetospec-d2` (`renderers/d2`, a module of its own
embedding the D2 library, ELK layout); the look is the diagram-design
skill's default skin (profile `codetospec`, `.diagram-design`).

## Revisions and commands

- codetospec branch `d2-diagrams` on `dabf223`; D2 library v0.7.2, the
  CLI v0.7.1 for the first probes; graphs: the remapped outputs of
  `remap-2026-11-07.md` under `~/spec-graphs`.
- Views: `bin/codetospec export --out ~/spec-graphs/<g> --to <dir>
  --format views --checkers <g>-checkers.json --capabilities-from
  ~/spec-graphs/admin`, with the review's checkers files (kept outside
  this repository).
- Agreement on the real models and domain-map timings: throwaway tests
  over the same graphs (not committed), MacBook (arm64), renderer
  parallel over the CPUs.

## D2 rendering

- ELK draws right-angle connectors with rounded corners (the skill
  requires them; dagre draws diagonals). Two renders of the same source
  are byte-identical (CLI and library; the renderer omits D2's version).
- Style values must be opaque `#rrggbb`, and explicit colours don't
  follow D2's dark theme: the dark rendering is a second source in the
  dark palette, shown by `<picture>` under `prefers-color-scheme: dark`.
- govulncheck flagged called vulnerabilities in D2's pinned `x/net`,
  `x/text` and `goldmark`; the renderer module requires fixed versions
  (none called after).

## Domain map

| graph | nodes | edges | drawing time | SVG |
|---|---|---|---|---|
| graphql-gateway | 51 | 11 | 0.6 s | 45 KB |
| admin | 67 | 28 | 1.5 s | 59 KB |
| assignments | 88 | 106 | 5.8 s | 105 KB |
| coaches | 110 | 182 | 16 s | 147 KB |
| districts | 158 | 186 | 27 s | 168 KB |
| users | 135 | 274 | 47 s | 199 KB |
| pkg | 86 | 378 | 119 s | 226 KB |
| ai-guide | 281 | 1117 | killed at 600 s | — |

Times are both palettes, in parallel. ELK's time grows faster than the
edges, so `d2.Draw` sends no diagram of more than 300 edges
(`d2.MaxEdges`): pkg and ai-guide keep their D2 source in the README,
as an undrawn map does. Domains no edge touches (most of admin's) went into
one grid container: before, ELK put them in one row, admin's map seven
times wider than tall; after, 3384 × 2035.

Like the Mermaid map before it, a map of 150 domains and 190 edges draws
but doesn't read; an overview with one detail per group is not built.

## Access views

| graph | rows | personas | columns | decision diagrams | not exported | time |
|---|---|---|---|---|---|---|
| admin | 37 | 134 | 23 | 33 | 4 | 7 s |
| users | 142 | 67 | 25 | 108 | 96 | 21 s |
| coaches | 160 | 62 | 31 | 137 | 69 | 24 s |
| assignments | 114 | 39 | 14 | 57 | 65 | 11 s |
| districts | 311 | 63 | 29 | 156 | 62 | 33 s |
| ai-guide | 168 | 107 | 24 | 129 | 62 | 26 s |

Rows are the exported operations plus item kinds: the export counts of
`policy-work-2026-11-08.md` (users 142, coaches 160, assignments 114,
districts 306 + 5 item kinds, ai-guide 167 + 1, admin 37). Merging the
personas decided alike cuts the columns 2 to 6 times. A decision diagram
has 1 to 5 questions (users: 56 with one, 25 two, 10 three, 15 four, 2
five), within the skill's nine nodes.

Agreement: every request the decider loaded for every persona on every
exported operation, checked against the rules' conditions
(`Operation.Cases`, each check's truth asked of cedar-go on that
request):

| graph | requests checked | disagreements | without a case |
|---|---|---|---|
| admin | 6968 | 0 | 536 |
| users | 22445 | 0 | 2278 |
| coaches | 18724 | 0 | 1426 |
| assignments | 6786 | 0 | 2223 |
| districts | 18144 | 0 | 19971 |
| ai-guide | 25894 | 0 | 4173 |

"Without a case": the operation has more than six checks
(`policy.MaxCaseLeaves`), so the conditions enumerate no cases to
compare with; the matrix still decides it through the exported policies.

## Not built, and why

- **Paired traces** (two callers on one operation, where they diverge):
  choosing the pair needs a question; the decision diagram and the
  matrix cover the single-caller case.
- **Role trees** (what granting a role opens): the matrix column answers
  it, and the skill prefers the table.
- **A figure of how the exports plug in** (enforcement point, policy,
  attributes, obligations): the loader contracts in the package docs
  state it; a hand-written D2 file would copy the palette `internal/d2`
  owns.
- **Domain map overview + detail** for large services: see above.

## Limits

- The matrix is what the export allows under the loader contract, not
  what webapp does: an operation left out of the exports is listed, not
  decided.
- A persona holds a role at one scope; grants combining scopes, and
  roles' implied capabilities beyond the role facts, are not modelled.
- D2's automatic layout can't follow the skill's 4 px grid or its label
  spacing rules (edge labels sit on their line); diagrams stay small
  instead.
