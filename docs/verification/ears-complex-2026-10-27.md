# EARS complex kind and one trigger on real graphs — 2026-10-27

## Question

codetospec gains a `complex` kind (two or more EARS keywords) and keeps
one trigger per rule, as Mavin's ruleset has it: `when` or `if`, never
both. The map and reduce models can no longer write both; `ears.Check`
now flags a sentence holding both ("two triggers"), reversing the
plan-5 decision that accepted "WHEN x, IF y, THEN …"
(`ears-clauses-2026-10-27.md`). How many existing rules does that touch,
and does the corpus move by exactly that?

## Revisions

- codetospec: `cf02426` plus the uncommitted work of 2026-10-26/27
  (structured EARS, `ears-components-2026-10-27.md`), with the complex
  kind and the two-trigger check.
- webapp (`~/khan/webapp`): `1ec3acc9594`; go1.27.2.
- Graphs: every `~/spec-graphs/*/nodes/rules/*.md`, read only.

## Commands

1. A throwaway test inside the module, deleted afterwards (`go test
   ./internal/render -run TestZZProbe -v`):

```go
func TestZZProbe(t *testing.T) {
	home, _ := os.UserHomeDir()
	graphs, _ := filepath.Glob(filepath.Join(home, "spec-graphs", "*"))
	type tally struct{ rules, parsed, complex, twoTriggers int }
	by := map[string]*tally{}
	perGraph := map[string]int{}
	combos := map[string]int{}
	for _, g := range graphs {
		paths, _ := filepath.Glob(filepath.Join(g, "nodes", "rules", "*.md"))
		for _, p := range paths {
			data, _ := os.ReadFile(p)
			n, err := render.ParseNode(string(data))
			if err != nil {
				continue
			}
			src := "llm"
			if n.Extra["extracted_by"] != "" {
				src = "extractor"
			}
			if n.Extra["inherited_from"] != "" {
				src = "inherited"
			}
			if by[src] == nil {
				by[src] = &tally{}
			}
			tl := by[src]
			tl.rules++
			req := graph.RuleRequirement(n.Body)
			c, err := ears.Parse(req)
			if err != nil {
				continue
			}
			tl.parsed++
			if ears.KindOf(&c) == ears.Complex {
				tl.complex++
				combos[strings.Join(ears.Patterns(&c), ", ")]++
			}
			if strings.HasPrefix(ears.Check(n.Extra["ears"], req), "two triggers") {
				tl.twoTriggers++
				perGraph[filepath.Base(g)]++
			}
		}
	}
	// … print by, combos, perGraph
}
```

2. `make corpus` against the recorded baselines, then
   `CODETOSPEC_CORPUS_UPDATE=1 make corpus`.

## Results

| Source | Rules | Parse | Complex | WHEN and IF |
| --- | ---: | ---: | ---: | ---: |
| LLM | 12,889 | 12,727 | 32 | 21 |
| extractor | 1,610 | 1,570 | 0 | 0 |
| inherited | 4,582 | 4,543 | 6 | 6 |

Combinations among the 38 complex rules: event + unwanted 27, optional
+ unwanted 6, state + unwanted 5. None combines state and event: the
old prompts listed the five single patterns only.

`make corpus` before re-recording: `ears_issues` rose in eight graphs —
pkg 13 → 15, admin 8 → 9, ai-guide 86 → 89, assignments 47 → 52,
coaches 24 → 28, districts 43 → 46, graphql-gateway 10 → 12, users
25 → 32 — 27 in all, graph for graph the probe's WHEN-and-IF count. No
other count moved; the baselines were re-recorded.

`TestExtractorRuleFactsPass` passes: no extractor template writes WHEN
and IF together, so no rule fact is dropped.

## Limits

- The 27 rules are flagged, not rewritten: they keep their components
  in graph.json (Parse still reads both triggers) until a run re-maps
  them under the new prompt.
- Older graphs declare the kind a sentence opens with; only rules
  written from components get `ears: complex`. `ears_patterns` is set on
  both, from the sentence.
