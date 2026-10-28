# ears-lint-go: what another EARS linter suggests — 2026-10-28

## Question

Does github.com/labeth/ears-lint-go — a standalone EARS linter: clause
parser, AND/OR/NOT trees inside conditions, a catalog of names with
aliases, coded diagnostics — find form defects codetospec misses, and
which of its checks would hold on real rules?

## Revisions

- codetospec: `cf02426` plus the uncommitted work of 2026-10-26/28
  (dirty tree), before the changes this record led to.
- ears-lint-go: `~/Documents/git/ears-lint-go` at `62e3d1d`.
- Graphs: every `~/spec-graphs/*/nodes/rules/*.md` (19,081 rules; 12,889
  written by the model, 12,727 of them read by `ears.Parse`), read only;
  go1.27.2.

## Commands

1. ears-lint-go's 28 cases (`corpus_test.go`) through `ears.Parse`,
   `ears.KindOf` and `ears.Check`; ears-lint-go itself
   (`LintEars(s, Catalog{}, &Options{CommaAsAnd: b})`, a throwaway module
   with a `replace`) on codetospec's own sentence forms.
2. A throwaway test (deleted) over the graphs, model-written rules only for
   the condition and response counts:

```go
req := graph.RuleRequirement(n.Body)
c, err := ears.Parse(req) // skip on err
conds := append(append(append([]string{}, c.Where...), c.While...), c.When, c.If)
// " or " in " "+lower(join(conds))+" "
// len(c.Shall) == 1 && (contains(lower(text), " and ") || contains(text, ","))
// whole sentence: unbalanced parentheses; (?i)\b(and|or|not|et|ou)\s*[,;];
// (?i)\b(and|or|not)\s+(and|or)\b; \s, (space before a comma)
```

3. 40 of each sample drawn with `rand.New(rand.NewPCG(1, 2))`, classified
   by hand.

## Results

Its 28 cases through codetospec:

| Result | Cases |
| --- | --- |
| Same kind | 16 valid, plus A303 (either accepted) |
| Read wrong (codetospec defect) | S801: spaces before the commas; the WHEN went into the WHILE text, kind `state`, and Check accepted it |
| Both refuse | I402, I404, I405, I406, N701 |
| Accepted on purpose | I410: WHEN and IF, two triggers |
| Accepted; refused there for form | I501 dangling "and,", I502 unclosed "(", I506 "or or" |
| Accepted; refused there by its catalog | A301: an alias of two systems |

On the graphs:

- Space before a comma: 0 rules. Unbalanced parentheses: 0. Doubled
  operators: 0. Dangling "and,"/"or,": 21, all after "shall" (response
  prose: "and, if present, …"); 7 of them hold an "if …" inside the
  response.
- " or " in a condition: 1,423 of 12,727. Of 40: about 33 inside a phrase
  ("an HTTP or parsing error occurs", "meets or exceeds") or an idiom
  ("one or more", "less than or equal"), about 7 between whole conditions.
- One response holding " and " or ",": 5,942 of 12,727. Of 40, 22 join two
  or more actions ("log an error and return …", "generate … and store
  …"); 18 are one action whose object is a list. About 3,300 rules (≈ 26 %)
  thus write several actions as one response, whose `ears_actions` keeps
  only the first.
- Its default vague words (appropriate, sufficient, as needed): 9 rules.

ears-lint-go on codetospec's forms: with an empty catalog, every sentence
is invalid in strict mode (the system does not resolve). It refuses
WHERE before WHILE (the order Render writes), WHEN with IF, Kiro's
comma-less "WHEN x THE SYSTEM SHALL y", and French; it reads the capital
AND. "A, B, or C fails" leaves an unparsed tail, or an operator error with
commas as AND.

Decisions: fix S801 (one function now decides what separates two
clauses, for Parse and Validate); ask the model for one `shall` item per
action; actor aliases and a list of unused actors, after its catalog;
its agreeing cases as a test (`internal/ears/earslint_test.go`). Not
taken: AND/OR/NOT trees, the compound-response and vague-word warnings,
form checks with nothing to catch, the library itself.

## Limits

- The graphs predate the structured `shall` list: a sentence could only
  split at "; ". Whether the model now writes one item per action is
  unmeasured until a real run; re-measure then with command 2 and a fresh
  40-sample.
- The samples are 40 rules each, classified by one reader: ± about 15
  points.
- The graphs hold no actor glossary: the value of aliases is unmeasured.
