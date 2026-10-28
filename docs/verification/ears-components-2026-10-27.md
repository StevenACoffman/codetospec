# EARS components on real graphs — 2026-10-27

## Question

If the map and reduce models write EARS as components (where, while,
when, if, shall; `ears.Components`) and codetospec derives the sentence
(`ears.Render`) and the kind (`ears.KindOf`), what happens to the rules
that exist only as sentences — extractor templates, inherited rules,
rules a person edited, older graphs? Does `ears.Parse` read them, and
does the kind it implies agree with the declared `ears:`?

## Revisions

- codetospec: `cf02426` plus the uncommitted work of 2026-10-26/27, with
  `internal/ears/components.go` as of this record.
- webapp (`~/khan/webapp`): `1ec3acc9594`; go1.27.2.
- Graphs: every `~/spec-graphs/*/nodes/rules/*.md`
  (admin, ai-guide, assignments, coaches, districts, districts-jobs,
  graphql-gateway, pkg, users), read only. Inherited rules repeat a
  shared rule in every graph that inherits it, so they are counted per
  graph.

## Commands

A throwaway test inside the module, deleted afterwards (`go test
./internal/render -run TestZZProbe -v`). Source by frontmatter:
`extracted_by` → extractor, `inherited_from` → inherited, else LLM.

```go
func TestZZProbe(t *testing.T) {
	home, _ := os.UserHomeDir()
	graphs, _ := filepath.Glob(filepath.Join(home, "spec-graphs", "*"))
	type tally struct{ rules, checkOK, parsed, roundTrip, kindAgree, kindAgreeCheckOK int }
	by := map[string]*tally{}
	errs := map[string]int{}
	samples := map[string][]string{}
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
			tl := by[src]
			if tl == nil {
				tl = &tally{}
				by[src] = tl
			}
			req := graph.RuleRequirement(n.Body)
			kind := n.Extra["ears"]
			tl.rules++
			checkOK := ears.Check(kind, req) == ""
			if checkOK {
				tl.checkOK++
			}
			c, err := ears.Parse(req)
			if err != nil {
				msg := err.Error()
				if i := strings.Index(msg, "\""); i > 0 && strings.HasPrefix(msg, "clause") {
					msg = "clause opens with no EARS keyword"
				}
				errs[src+": "+msg]++
				if len(samples[src+": "+msg]) < 3 {
					samples[src+": "+msg] = append(samples[src+": "+msg], filepath.Base(g)+": "+req)
				}
				continue
			}
			tl.parsed++
			lang := "en"
			if strings.Contains(strings.ToLower(req), "doit") {
				lang = "fr"
			}
			if back, err := ears.Parse(ears.Render(&c, lang)); err == nil && reflect.DeepEqual(back, c) {
				tl.roundTrip++
			}
			if ears.KindOf(&c) == kind {
				tl.kindAgree++
				if checkOK {
					tl.kindAgreeCheckOK++
				}
			} else if checkOK {
				key := src + ": check ok, kind " + kind + " -> " + ears.KindOf(&c)
				errs[key]++
				if len(samples[key]) < 3 {
					samples[key] = append(samples[key], filepath.Base(g)+": "+req)
				}
			}
		}
	}
	for src, tl := range by {
		fmt.Printf("%s: %+v\n", src, *tl)
	}
	keys := make([]string, 0, len(errs))
	for k := range errs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%6d %s\n", errs[k], k)
		for _, s := range samples[k] {
			if len(s) > 220 {
				s = s[:220] + "…"
			}
			fmt.Printf("         %s\n", s)
		}
	}
}
```

## Results

| Source | Rules | Pass `ears.Check` | Parse | Round trip¹ | Parsed kind = declared |
| --- | ---: | ---: | ---: | ---: | ---: |
| LLM | 12,889 | 12,724 | 12,727 (98.7 %) | 12,727 | 12,692 |
| extractor | 1,610 | 1,570 | 1,570 | 1,570 | 1,570 |
| inherited | 4,582 | 4,531 | 4,543 | 4,543 | 4,525 |

¹ `Parse(Render(Parse(s)))` = `Parse(s)`.

Sentences that do not parse:

- 155 LLM and 39 inherited: more than one "shall" ("…; otherwise, the
  system shall …", two WHEN cases in one rule). `ears.Check` already
  flags them; as components, the model has to split the rule or write
  the second case as a response.
- 7 LLM: "WHEN x, IF y, the system shall" — IF without THEN.
- 40 extractor (ai-guide): "The system does not check access to … yet"
  (`acl.TODO()`), no modal; these graphs predate the rule-fact
  validation that now rejects such facts.

Kind: 31 rules that pass `ears.Check` change kind under `KindOf` —
all of them stacked "WHEN/WHILE/WHERE x, IF y, THEN …" declared event
(22), optional (4) or state (5); `KindOf` calls them unwanted. Both
declarations pass `Check`; `KindOf` picks one deterministically.

## Decision

Parse covers 98.7 % of real sentences and reads back every one it
parses, so graph.json can carry the components of every rule, whoever
wrote it, derived from the sentence (the node file stays the source a
person edits). A sentence that does not parse gets no components rather
than a guess. The map and reduce models write components; their
sentences pass `ears.Check` by construction (`FuzzRenderParse`).

## Limits

- No model was called: how often the model's components need a
  correction, and how the derived sentences compare with today's in
  crosscheck verdicts, needs a real run (a full re-map: the prompts
  change, so every cache key does).
- The kind change of the 31 stacked rules is a choice, not a
  measurement of which kind a reader expects.
