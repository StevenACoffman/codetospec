# Test reach through interfaces, and executed lines — 2026-10-26

## Question

1. Static test reach (`tests`) misses code a test runs through a module
   interface. How many rules does that hide, and does adding the
   interface links (`tests_via_interface`) change any existing count?
2. How does static reach compare with what a test run executed, read
   from a coverage profile (`executed`)?

## Revisions

- codetospec: `cf02426` plus the uncommitted change adding interface
  dispatch, `executed` facts and the `questions` ledger.
- webapp (`~/khan/webapp`): `1ec3acc9594` (2026-10-17), clean.
- districts-jobs: `eda9a0cfe` (2026-10-09), clean.
- go1.27.2 darwin/arm64.
- Graphs: the rendered outputs in `~/spec-graphs/{assignments,districts}`
  and `~/spec-graphs/districts-jobs/{roster,pkg,distutil}`, read only.

## Commands

```sh
(cd extractors/go && go build -o /tmp/extract .)
# For each source: the facts with interface dispatch.
(cd ~/khan/webapp/services/assignments && /tmp/extract -root . -skip-dirs testutil > facts.json)
# Coverage profiles (districts-jobs modules only; webapp tests need services).
(cd ~/Documents/git/districts-jobs/roster &&
  go test -count=1 -coverpkg=./... -coverprofile=/tmp/roster.cover ./...)
(cd ~/Documents/git/districts-jobs/roster &&
  /tmp/extract -root . -skip-dirs testutil -cover /tmp/roster.cover > facts.json)
```

The counts come from a throwaway program, built inside the module (it
imports `internal/`) and deleted afterwards: it loads a graph with
`render.LoadDir`, the facts with `extract.LoadFactsFile`, runs
`testreach.Annotate` once with the dispatch facts removed and once with
all of them, and counts the rule fields.

## Results

Extractor characterization: on distutil, isyncso and pkg of
districts-jobs, every fact of the previous extractor is still emitted
byte for byte; the only new facts are call and test facts marked
`dispatch: interface` (pkg: 142 call, 28 test; distutil and isyncso:
none).

Test reach (rules with a known `tests`):

| Graph       | Rules | Reached directly | `tests: 0` but reached through an interface | With `tests_via_interface` |
| ----------- | ----: | ---------------: | ------------------------------------------: | -------------------------: |
| assignments | 1,261 |              611 |                                          40 |                         61 |
| districts   | 2,773 |            1,447 |                                           8 |                         13 |
| roster      |   561 |              319 |                                           0 |                          0 |
| pkg         |   412 |              138 |                                           1 |                          1 |

The "reached directly" counts equal those measured when test reach was
added (assignments 611, districts 1,447, roster 319): the new field
changes no existing number. In assignments the interface callees are
mostly gqlgen resolvers, genqlient queries and a test double
(`testutil/write_failure_client.go`), which is why drift's callees
ignore dispatch links.

Static reach against execution (rules with both fields):

| Graph    | Reached and executed | Reached, not executed | Executed, not reached | Neither |
| -------- | -------------------: | --------------------: | --------------------: | ------: |
| roster   |                  305 |                    14 |                    12 |     227 |
| distutil |                   40 |                     0 |                     1 |      74 |

Roster: 558 of its 561 rules have every cited file in the profile; 317
have cited code the tests executed.

## Limits

- Coverage was measured on districts-jobs only; the webapp services'
  tests need running services.
- A profile says what ran, not which test ran it, nor that a test
  asserted on it.
- "Executed, not reached" are code paths static reach cannot follow
  (deeper than three calls, through interfaces of other modules, or by
  reflection); "reached, not executed" are uses the run did not take.
  Neither set was inspected rule by rule.
