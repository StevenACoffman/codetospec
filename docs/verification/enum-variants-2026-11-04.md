# Near-duplicates that vary along one enumeration — 2026-11-04

## Question

`DOMAIN_PLAN.md` recommendation 17: rules that differ along one dimension
(finding 5: content level, course / unit / lesson, 13 of assignments' 67
pairs) should become one parameterized entry. Can the code's own
enumerations (`enum_value` facts) find that dimension, and on how many
pairs?

## Revisions

- codetospec `2141826` plus this session's changes; `~/spec-graphs/assignments`
  and `~/spec-graphs/coaches`, read only.

## Command

Disposable Python: the near-duplicate pairs exactly as
`consistency.FindDuplicates` makes them (requirement words of ≥ 3
letters, Jaccard ≥ 0.8, equal negation counts, not both mechanical:
reproduces the README's 67 and 66); for each pair, the words one
requirement has and the other lacks; a pair "varies along" an enum when
those words are all among its values' words (the value's name minus its
enum's name, camelCase split, and its literal value).

## Result

| | pairs | along one enum | enums |
|---|---:|---:|---|
| assignments | 67 | 8 | `AssignmentSubtype` 7, `AssignmentStatus` 1 |
| coaches | 66 | 3 | `RosterChangeType` 2, `CoachType` 1 |

The `AssignmentSubtype` pairs are content level: `UNIT`,
`CourseChallenge` give the words "unit" and "course" (course mastery vs
unit mastery entity ids, course vs unit authorization of content data).

## Reading

- Enumerations find about half of the content-level pairs the plan
  counted by hand (7 of 13); the rest name the level in strings, not in
  an enum.
- 11 pairs over two services: too few to justify a variants mechanism
  now. Deferred (`TODO.md`); revisit when a confirmed model names the
  dimension (a modelith enum), which would find the string cases too.

## Limits

- Two services; no hand check beyond three pairs.
