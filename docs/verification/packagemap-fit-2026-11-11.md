# Fitting large package maps to be drawn, 2026-11-11

After the remap of 2026-11-11, three package maps had no picture. Each had
more edges than `d2.MaxEdges` (300), past which ELK takes minutes, so the
README showed only the D2 source:

- webapp pkg: 424 edges;
- districts-jobs roster: 372;
- ai-guide: 1,173.

## The options, measured on those three maps

| map      | edges | grouped by directory         | transitive reduction | grouped, then reduced |
| -------- | ----- | ---------------------------- | -------------------- | --------------------- |
| roster   | 372   | 45 at 2 levels (30 groups)   | 146                  | —                     |
| pkg      | 424   | 62 at 1 level (17 groups)    | 125                  | —                     |
| ai-guide | 1,173 | 693 at 2 levels (102 groups) | 348                  | 151 at 2 levels       |

- Grouping alone is coarse. pkg fits only at 1 level, and ai-guide fits
  at no depth that keeps its directories apart.
- The transitive reduction leaves out each edge a longer path already
  implies, so every dependency still reads along a path. It removes 60
  to 70 % of the edges, and is enough for pkg and roster. Go imports are
  acyclic, so the reduction is unique.

## What `packagemap.Build` does now (`fit.go`)

- A map of at most 300 edges is drawn as it was. No other output changed.
- Over 300, the implied edges are left out. The reduction runs in order,
  dropping an edge only when its source still reaches its target without
  it, so it stays deterministic and safe where grouping makes cycles.
- If the map is still over 300, packages are grouped by directory at the
  deepest level whose reduced map fits. A directory holding one package
  stays that package, and a group's label names how many packages it
  holds.
- The README intro says which of these happened.

## Results

The three outputs were regenerated from their caches (0 tokens):

| map      | drawn edges | nodes                | left out                   |
| -------- | ----------- | -------------------- | -------------------------- |
| pkg      | 125         | 105                  | 299 implied                |
| roster   | 146         | 85                   | 226 implied                |
| ai-guide | 151         | 103 groups, 2 levels | 542 implied after grouping |

`render.Meta` carries the two counts as plain integers. Importing
`packagemap` into `render` raised package NCCD (2.597403 → 2.606061) and
failed the architecture gate.
