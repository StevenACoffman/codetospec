# Kiro: EARS as Kiro writes it, and the Kiro export — 2026-10-27

## Question

Does anything in how Kiro writes EARS call for a change to codetospec,
and does an export of codetospec rules as Kiro specs keep every
requirement — its EARS sentence readable back into the rule's
components?

## Revisions

- codetospec: `cf02426` plus the uncommitted work of 2026-10-26/27, with
  the actor, action and Kiro export changes of this record.
- Kiro: `~/Documents/git/Kiro` at `bfe7ff3` — the public issue tracker
  (no Kiro source: no EARS parser or writer to read); its one generated
  spec `.kiro/specs/github-issue-automation/requirements.md`. Kiro's
  documented form, kiro.dev/docs/specs/concepts (via a search result; the
  page itself answered 404 to a direct fetch): "WHEN [condition/event]
  THE SYSTEM SHALL [expected behavior]".
- Graphs: `~/spec-graphs/districts` and `~/spec-graphs/assignments`, read
  only; go1.27.2.

## Commands

1. The sample's criteria: `grep -E '^[0-9]+\. '` of requirements.md, each
   through `ears.Parse`, `ears.Check(ears.KindOf(c), s)` and Render (a
   throwaway test, deleted); counts by `grep`.
2. `bin/codetospec export --out ~/spec-graphs/<g> --to <scratch>/kiro-<g>
   --format kiro` for districts and assignments, then a throwaway test
   (deleted) matching each criterion to its rule by the trace comment:

```go
// TestZZProbe: every exported criterion, read back, has its rule's
// components (actions aside); an unparsed one is kept as written.
func TestZZProbe(t *testing.T) {
	home, _ := os.UserHomeDir()
	trace := regexp.MustCompile(`<!-- codetospec: (\S+) ·[^\n]*-->\n\n#### Acceptance Criteria\n\n1\. ([^\n]*)\n`)
	for _, g := range strings.Split(os.Getenv("GRAPHS"), ",") {
		nodes, err := render.LoadDir(filepath.Join(home, "spec-graphs", g))
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]*graph.Node{}
		for i := range nodes {
			byID[nodes[i].ID] = &nodes[i]
		}
		files, _ := filepath.Glob(filepath.Join(os.Getenv("EXPORTS"), "kiro-"+g, ".kiro/specs/*/requirements.md"))
		var total, same, asWritten, differ, properties int
		for _, f := range files {
			data, _ := os.ReadFile(f)
			for _, m := range trace.FindAllStringSubmatch(string(data), -1) {
				total++
				rule := byID[m[1]]
				want, ok := graph.RuleComponents(rule)
				if !ok {
					if m[2] == strings.TrimSuffix(strings.Join(strings.Fields(graph.RuleRequirement(rule.Body)), " "), ".") {
						asWritten++
					}
					continue
				}
				got, err := ears.Parse(m[2])
				want = ears.WithActions(&want, nil)
				if err == nil && reflect.DeepEqual(got, want) {
					same++
				} else {
					differ++
					if differ <= 3 {
						fmt.Printf("  DIFF %s\n    %s\n", m[1], m[2])
					}
				}
			}
			design, err := os.ReadFile(filepath.Join(filepath.Dir(f), "design.md"))
			if err == nil {
				properties += strings.Count(string(design), "\n**Validates: Requirements ")
			}
		}
		fmt.Printf("%s: specs %d, criteria %d, read back the same %d, as written (unparsed) %d, differ %d, properties %d\n",
			g, len(files), total, same, asWritten, differ, properties)
	}
}
```

## Results

The Kiro sample:

- 41 criteria: 30 open with WHEN, 11 are ubiquitous; none has a WHILE,
  WHERE or IF clause (3 hold a lowercase "if" inside the response).
- All 41 parse and pass `ears.Check`. Before this change their actor
  read back as "tHE Issue_Manager" (only the first letter lowered), which
  Validate did not catch; now "the Issue_Manager", and RenderCapitals
  writes the sentence back as Kiro wrote it (plus a final period).
- Actors: 39 of 41 name a glossary term (Issue_Manager 11, Label_Assigner
  9, Duplicate_Closer 5, Stale_Issue_Handler 5, Bedrock_Classifier 4,
  GitHub_Workflow 3, Duplicate_Detector 2); 2 name "THE comment", which
  the glossary lacks.
- 8 criteria put an unwanted situation under WHEN (7 failures, 1 rate
  limit approached) where EARS uses IF … THEN; codetospec classes them
  event. Not changed: telling them apart means guessing from words like
  "fails" (keyword checks measured ~25 % precision,
  `ears-clauses-2026-10-27.md`).
- Kiro traces by `<requirement>.<criterion>` (design.md "**Validates:
  Requirements 1.1, 5.5**", tasks.md "_Requirements: 5.1_").

The export:

| Graph | Specs | Requirements | Read back the same | Kept as written (unparsed) | Differ | Properties |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| districts | 152 | 3,556 | 3,514 | 42 | 0 | 456 |
| assignments | 43 | 1,273 | 1,231 | 42 | 0 | 226 |

No rule was refuted (these graphs carry no crosscheck verdicts). Few
rules have correctness properties: most rules of these graphs have no
acceptance criteria (reduced in merge mode).

## Decisions

- Actor casing fixed (the article lowered, the name kept).
- Allowed actors: a glossary (`actors:` in codetospec.yaml), as Kiro's,
  so the model cannot coin synonyms; with none, every rule states "the
  system".
- Export: one Kiro spec per domain, a requirement per rule, its EARS
  sentence the criterion N.1 in Kiro's capitals, its acceptance criteria
  the property validating N.1; no user story (none is stated by code),
  no tasks.md.

## Limits

- One Kiro spec is a small sample of how Kiro writes EARS.
- Kiro does not validate the files against a schema; whether Kiro's
  agent uses an exported spec well needs Kiro itself.
