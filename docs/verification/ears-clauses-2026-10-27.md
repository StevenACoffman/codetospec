# EARS clauses on real graphs — 2026-10-27

## Question

Rules may leave out an EARS clause (WHILE, WHEN, WHERE, IF … THEN) and
claim a wider scope than the code has, or state the condition after
"shall" where no check looks. Is either common enough to justify a new
`ears.Check` rule, a map prompt change (stacked clauses), or a check of
the guard around the cited lines?

## Revisions

- codetospec: `cf02426` plus the uncommitted work of 2026-10-26/27;
  `ears.Check` itself unchanged.
- webapp (`~/khan/webapp`): `1ec3acc9594`; districts-jobs: `eda9a0cfe`.
- Graphs: `~/spec-graphs/{pkg,admin,assignments,coaches,districts,graphql-gateway,users,ai-guide}`
  and `~/spec-graphs/districts-jobs/{roster,pkg,distutil}`, read only.
  They predate `ears_issue`, so issues were recomputed with the current
  `ears.Check` (as `make corpus` does).

## Commands

1. Conditions after "shall": every ubiquitous or event rule of districts,
   assignments and roster whose text after the first " shall " holds
   *if, when, unless, while* or *whenever*; 60 drawn with
   `random.seed(7); random.sample(rows, 60)` and classified by hand.
2. `ears.Check` over each graph's rules: a throwaway program inside the
   module (`render.LoadDir`, then `ears.Check(n.Extra["ears"],
   graph.RuleRequirement(n.Body))`), deleted afterwards.
3. "Otherwise" compounds: LLM rules (no `extracted_by`, no
   `inherited_from`) of districts, assignments, roster, users and coaches
   with one "shall" and *otherwise* or *else* after it, leaving out
   "true/false/nil/null/empty/zero otherwise"; 30 sampled, seed 11.
4. Uncited guards: a standalone program parsing the cited Go files with
   `go/parser`, flagging an LLM rule when every citation lies inside one
   `if` body or `case` clause (the condition itself not cited):

```go
// Probe: rules whose every citation lies inside one if/case body, so the
// guard's condition is not cited.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type body struct{ cond string; condLine, start, end int }

var cache = map[string][]body{}

func bodies(path string) []body {
	if b, ok := cache[path]; ok { return b }
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	var out []body
	if err == nil {
		src, _ := os.ReadFile(path)
		text := func(n ast.Node) string { return string(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset]) }
		ast.Inspect(f, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.IfStmt:
				out = append(out, body{text(s.Cond), fset.Position(s.Pos()).Line, fset.Position(s.Body.Lbrace).Line + 1, fset.Position(s.Body.Rbrace).Line - 1})
			case *ast.CaseClause:
				if len(s.List) > 0 && len(s.Body) > 0 {
					out = append(out, body{"case " + text(s.List[0]), fset.Position(s.Pos()).Line, fset.Position(s.Colon).Line + 1, fset.Position(s.End()).Line})
				}
			}
			return true
		})
	}
	cache[path] = out
	return out
}

var srcRe = regexp.MustCompile(`(?m)^  - path: "?([^"\n]+)"?\n    lines: "?(\d+)-(\d+)"?`)
var kindRe = regexp.MustCompile(`(?m)^ears: (\S+)`)
var reqRe = regexp.MustCompile(`\*\*Requirement \(EARS\)\*\*: (.*)`)

func main() {
	graph, src := os.Args[1], os.Args[2]
	files, _ := filepath.Glob(filepath.Join(graph, "nodes/rules/*.md"))
	type hit struct{ kind, req, cond, file string }
	var hits []hit
	total := map[string]int{}
	flagged := map[string]int{}
	for _, f := range files {
		data, _ := os.ReadFile(f)
		t := string(data)
		k := kindRe.FindStringSubmatch(t)
		r := reqRe.FindStringSubmatch(t)
		srcs := srcRe.FindAllStringSubmatch(t, -1)
		if k == nil || r == nil || len(srcs) == 0 || strings.Contains(t, "extracted_by") || strings.Contains(t, "inherited_from") { continue }
		total[k[1]]++
		cond, all := "", true
		for _, s := range srcs {
			if !strings.HasSuffix(s[1], ".go") { all = false; break }
			a, _ := strconv.Atoi(s[2]); b, _ := strconv.Atoi(s[3])
			var inner *body
			for i, bd := range bodies(filepath.Join(src, s[1])) {
				if bd.start <= a && b <= bd.end && (inner == nil || bd.start >= inner.start) {
					inner = &bodies(filepath.Join(src, s[1]))[i]
				}
			}
			if inner == nil { all = false; break }
			cond = inner.cond
		}
		if all {
			flagged[k[1]]++
			hits = append(hits, hit{k[1], r[1], cond, filepath.Base(f)})
		}
	}
	fmt.Println("total", total, "flagged", flagged)
	rand.New(rand.NewSource(3)).Shuffle(len(hits), func(i, j int) { hits[i], hits[j] = hits[j], hits[i] })
	for i, h := range hits[:min(40, len(hits))] {
		fmt.Printf("%d [%s] cond=%q\n   %s\n", i, h.kind, strings.Join(strings.Fields(h.cond), " "), h.req)
	}
}
```

   Run as `go run . ~/spec-graphs/districts ~/khan/webapp/services/districts`.
5. Extractor rule facts: `graph.ValidateRuleFacts` over
   `extractors/webapp/testdata/golden*.json` (now
   `TestExtractorRuleFactsPass`).

## Results

Conditions after "shall" (1): 655 hits. The 60 sampled, by hand:

| Reading                                                         | Rules |
| --------------------------------------------------------------- | ----: |
| Defining predicate ("shall consider X … if Y", "return true if") |   ~22 |
| Compound response, one case split ("X if A, otherwise Y")        |   ~20 |
| A guard that belongs in a clause ("add the header if non-empty") |   ~14 |
| Keyword noise ("while maintaining order"), restrictions          |    ~4 |

As an `ears_issue`, a keyword rule would be right about a quarter of the
time. The webapp extractor's own templates put "only when" after
"shall" (`contract.go`, `entrypoints.go`, `permit.go`); a failing
extractor rule fact is dropped, so such a check would delete those rules.

`ears.Check` today (2), rules flagged by reason:

| Graph           | Rules | Two "shall" | No "shall" | Wrong opening |
| --------------- | ----: | ----------: | ---------: | ------------: |
| pkg             | 1,437 |          11 |          0 |             2 |
| admin           | 1,038 |           6 |          0 |             2 |
| assignments     | 1,273 |          11 |         30 |             6 |
| coaches         | 2,283 |          16 |          5 |             3 |
| districts       | 3,556 |          40 |          0 |             3 |
| graphql-gateway |   883 |           8 |          0 |             2 |
| users           | 2,758 |          23 |          0 |             2 |
| ai-guide        | 5,853 |          79 |          5 |             2 |
| roster          |   784 |          14 |          0 |             0 |
| districts-jobs pkg |  412 |           9 |          0 |             0 |
| distutil        |   210 |           5 |          0 |             0 |

The two-"shall" rules are compound responses ("…; otherwise, the system
shall …"). The 30 "no shall" in assignments are an older extractor
template ("The system does not check access …"), since replaced.

"Otherwise" compounds without a second "shall" (3): 102 of 6,831 LLM
rules (1.5%); with two "shall": 81 (1.2%). Most sampled ones compute one
value with a case split ("set the grade to A if a student, otherwise B").

Uncited guards (4), districts: 39 of 2,346 LLM rules (event 7,
ubiquitous 1 of 222, unwanted 31). Most unwanted ones state their own
inner condition; the uncited one is an outer `err == nil` or a flag.

Stacked clauses already occur and pass: WHEN + IF … THEN, WHILE + IF …
THEN, WHERE + IF … THEN, and WHILE + WHEN.

Extractor goldens (5): 51 + 51 + 22 rule facts, none rejected.

## Decision

No new EARS check, no prompt change, no guard check: each would act on
1–2% of rules, or flag mostly correct ones. Kept instead: a test that the
extractor's rule facts pass the core's validation, and tests pinning the
stacked forms and conditions after "shall" that `ears.Check` accepts.
A map-time correction for two-"shall" replies (the `reground` exchange)
would touch about 1.2% of rules; deferred.

## Limits

- One person's hand classification of 60 + 30 rules; the categories
  overlap at the edges.
- Keyword searches are English-only (all graphs measured are English).
- The guard probe reads Go only and the innermost enclosing body only.
