# EARS generic syntax (triggers, preconditions, actor) on real graphs — 2026-10-27

## Question

EARS's generic syntax is `[Trigger] [Precondition] Actor Action
[Object]`, and a complex requirement is `<Multiple Conditions>, the
<system name> shall <system response>`, several triggers included:
"When the landing gear button is depressed once, if the software detects
that the landing gear does not lock into position, then the software
shall sound an alarm." is valid. `ears-complex-2026-10-27.md` had made
WHEN and IF together an issue ("two triggers") and the model's output
invalid; and only "the system" was accepted as the actor. With both
reversed — two triggers allowed, any actor "the <system name>",
conditions in any order (IF … THEN last), preconditions joined to a
trigger by a capital AND — does any existing rule's `ears.Check` verdict
get worse, and how much do Parse and the corpus move?

## Revisions

- codetospec: `cf02426` plus the uncommitted work of 2026-10-26/27,
  before and after this change to `internal/ears`.
- webapp (`~/khan/webapp`): `1ec3acc9594`; go1.27.2.
- Graphs: every `~/spec-graphs/*/nodes/rules/*.md` (19,081 rules), read
  only.

## Commands

1. Before the change, and again after it, a throwaway test inside the
   module (deleted afterwards) dumped each rule's verdict:

```go
// TestZZDump writes, per rule file, the source, the kind, the sentence,
// Check's verdict and whether Parse reads it, to CHECK_DUMP.
func TestZZDump(t *testing.T) {
	home, _ := os.UserHomeDir()
	paths, _ := filepath.Glob(filepath.Join(home, "spec-graphs", "*", "nodes", "rules", "*.md"))
	type row struct{ Src, Kind, Req, Issue string; Parsed bool }
	out := map[string]row{}
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
		req := graph.RuleRequirement(n.Body)
		_, perr := ears.Parse(req)
		out[p] = row{src, n.Extra["ears"], req, ears.Check(n.Extra["ears"], req), perr == nil}
	}
	data, _ := json.Marshal(out)
	_ = os.WriteFile(os.Getenv("CHECK_DUMP"), data, 0o600)
}
```

   run as `CHECK_DUMP=<file> go test ./internal/render -run TestZZDump`,
   then the two dumps compared rule by rule (passed before and fails
   after = regression; parsed before and not after = lost).
2. `make corpus` against the baselines `ears-complex-2026-10-27.md`
   recorded, then `CODETOSPEC_CORPUS_UPDATE=1 make corpus`.

## Results

- Regressions: 0 of 19,081. Rules that pass again: 27 (21 LLM, 6
  inherited) — exactly the "two triggers" of the previous record, and no
  other.
- Parse: unchanged per source (LLM 12,727 of 12,889, extractor 1,570 of
  1,610, inherited 4,543 of 4,582); none lost, none gained.
- Actors: no existing sentence names an actor other than "the system"
  (none gained a parse or a passing verdict from it): the old prompts
  wrote only "the system".
- Corpus: `ears_issues` back to the baselines before
  `ears-complex-2026-10-27.md` (pkg 13, admin 8, ai-guide 86,
  assignments 47, coaches 24, districts 43, graphql-gateway 10, users
  25); re-recording left `internal/corpus/testdata/corpus.json`
  identical to the committed file.

## Limits

- A precondition joined to the trigger by a lowercase "and" ("When an
  Order is shipped and Order Terms are not “Prepaid”") stays in the
  trigger's text: it cannot be told from a compound trigger ("signs up
  and confirms the email"). Only components written by the model, or a
  capital AND, separate them.
- Action and object are not separated: EARS's templates name one
  "system response", and the verb boundary is not recoverable from a
  sentence (adverbs, phrasal verbs, negation).
- Whether the model names actors usefully, and how it splits triggers
  from preconditions, needs a real run.
