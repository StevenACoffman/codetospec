package questions_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/alignment"
	"github.com/StevenACoffman/codetospec/internal/concept"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/questions"
)

// ruleID is the rule the tests raise questions about.
const ruleID = "rule.billing.prorata"

// gap is a failed map chunk of billing in path.
func gap(path string) graph.CoverageGap {
	return graph.CoverageGap{
		Domain: "billing",
		Reason: graph.GapMapFailed,
		Path:   path,
		Lines:  "1-9",
	}
}

// ungrounded is a rule stating a value absent from its citation, found at
// where elsewhere, on cited code of digest.
func ungrounded(where, digest string) graph.Node {
	return graph.Node{ID: ruleID, Type: graph.TypeRule, Extra: map[string]string{
		"grounding":         "ungrounded",
		"grounding_missing": "30 @" + where + `; "PRO"`,
		graph.ExtraDigest:   digest,
	}}
}

// only is the one question of nodes.
func only(t *testing.T, nodes []graph.Node) questions.Question {
	t.Helper()
	qs := questions.List(nodes, nil, nil)
	if len(qs) != 1 {
		t.Fatalf("questions = %+v, want one", qs)
	}
	return qs[0]
}

// TestList: each finding is a question; its id does not move when only
// where the value occurs elsewhere moves, and moves when the cited code
// changes, so that a resolution lapses with it.
func TestList(t *testing.T) {
	nodes := []graph.Node{
		{ID: ruleID, Type: graph.TypeRule, Extra: map[string]string{
			graph.ExtraEARSIssue:    "event requirement does not open with WHEN",
			"crosscheck":            "unsupported",
			graph.ExtraStaleVerdict: "supported",
		}},
		{
			ID:    "domain.billing",
			Type:  graph.TypeDomain,
			Extra: map[string]string{"crosscheck": "unsupported"},
		},
	}
	gaps := []graph.CoverageGap{
		{Domain: "billing", Reason: graph.GapMapFailed, Path: "a.php", Lines: "1-9"},
	}
	var kinds []string
	for _, q := range questions.List(nodes, gaps, nil) {
		kinds = append(kinds, q.Kind+" "+q.Subject)
	}
	slices.Sort(kinds)
	want := []string{
		"coverage_gap billing",
		"ears_issue " + ruleID,
		"refuted " + ruleID,
		"stale_verdict " + ruleID,
	}
	if !slices.Equal(kinds, want) {
		t.Errorf("questions = %v, want %v", kinds, want)
	}

	first := only(t, []graph.Node{ungrounded("a.php:4", "d1")})
	if moved := only(t, []graph.Node{ungrounded("a.php:7", "d1")}); moved.ID != first.ID {
		t.Errorf("id moved with the value's other location: %s → %s", first.ID, moved.ID)
	}
	if changed := only(t, []graph.Node{ungrounded("a.php:4", "d2")}); changed.ID == first.ID {
		t.Errorf("id %s kept although the cited code changed", first.ID)
	}
}

// TestApply: a resolution is attached to its question and makes its
// finding resolved, a rule's or a coverage gap's; one matching no
// question is listed as unused.
func TestApply(t *testing.T) {
	nodes := []graph.Node{ungrounded("a.php:4", "d1")}
	q := only(t, nodes)
	resolution := questions.Resolution{
		Disposition: questions.Verified,
		Reason:      "the 30 is the contract's",
	}
	ledger := questions.Apply(questions.List(nodes, nil, nil), map[string]questions.Resolution{
		q.ID:             resolution,
		"q-000000000000": resolution,
	})
	if got := ledger.Questions[0].Resolution; got == nil || *got != resolution {
		t.Errorf("resolution = %v", got)
	}
	if !ledger.ResolvedRule(questions.KindUngrounded, ruleID) || ledger.Count() != 1 {
		t.Errorf("finding not resolved: %+v", ledger)
	}
	if !slices.Equal(ledger.Unused, []string{"q-000000000000"}) {
		t.Errorf("unused = %v", ledger.Unused)
	}

	gaps := []graph.CoverageGap{gap("a.php"), gap("b.php")}
	listed := questions.List(nil, gaps, nil)
	ledger = questions.Apply(listed, map[string]questions.Resolution{listed[0].ID: resolution})
	resolvedGap := slices.IndexFunc(gaps, func(g graph.CoverageGap) bool {
		return g.String() == listed[0].Detail
	})
	for i := range gaps {
		if got := ledger.ResolvedGap(&gaps[i]); got != (i == resolvedGap) {
			t.Errorf("gap %s resolved = %v", gaps[i].String(), got)
		}
	}
}

// TestLoad: no file is no resolution; an entry without a question id, a
// known disposition or a reason fails with the file's name.
func TestLoad(t *testing.T) {
	cases := map[string]struct {
		content string // "" for no file
		want    int
		err     string
	}{
		"no file": {},
		"valid": {
			content: "q-0123456789ab:\n  disposition: verified\n  reason: checked\n",
			want:    1,
		},
		"empty file": {content: "\n"},
		"not an id": {
			content: "Q-1:\n  disposition: verified\n  reason: x\n",
			err:     "not a question id",
		},
		"disposition": {
			content: "q-0123456789ab:\n  disposition: fixed\n  reason: x\n",
			err:     "disposition",
		},
		"no reason": {
			content: "q-0123456789ab:\n  disposition: out-of-scope\n",
			err:     "needs a reason",
		},
		"unknown key": {
			content: "q-0123456789ab:\n  disposition: verified\n  reason: x\n  by: me\n",
			err:     "by",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.content != "" {
				path := filepath.Join(dir, questions.ResolutionsFile)
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := questions.Load(dir)
			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("Load: %v", err)
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err) ||
				!strings.Contains(err.Error(), questions.ResolutionsFile)):
				t.Fatalf("error = %v, want one naming the file and %q", err, tc.err)
			case len(got) != tc.want:
				t.Errorf("resolutions = %v, want %d", got, tc.want)
			}
		})
	}
}

// technical is the nature of a non-functional rule, performance one of
// its qualities, and nature the frontmatter key of the nature.
const (
	technical   = "technical"
	performance = "performance_efficiency"
	nature      = "nature"
	business    = "business"
)

// TestListNFRTarget: a non-functional rule stating a quantity asks for
// its target until its code states a threshold; one naming no quantity, a
// business rule serving a quality, and a technical one of plain
// functionality ask nothing.
func TestListNFRTarget(t *testing.T) {
	cases := map[string]struct {
		nature, attribute, threshold, quantity string
		want                                   []string
	}{
		"a quantity stated, no threshold": {
			nature: technical, attribute: performance, quantity: graph.QuantityStated,
			want: []string{"nfr_target performance_efficiency"},
		},
		"no quantity named": {nature: technical, attribute: performance},
		"threshold stated in code": {
			nature: technical, attribute: performance, threshold: "30 seconds",
		},
		"business rule serving security": {nature: business, attribute: "security"},
		"plain functionality": {
			nature:    "technical",
			attribute: "functional_suitability",
		},
		"not classified": {nature: technical},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			extra := map[string]string{nature: tc.nature}
			if tc.attribute != "" {
				extra[graph.ExtraQualityAttribute] = tc.attribute
			}
			if tc.threshold != "" {
				extra[graph.ExtraThreshold] = tc.threshold
			}
			if tc.quantity != "" {
				extra[graph.ExtraQuantity] = tc.quantity
			}
			var got []string
			for _, q := range questions.List(
				[]graph.Node{{ID: ruleID, Type: graph.TypeRule, Extra: extra}}, nil, nil) {
				got = append(got, q.Kind+" "+q.Detail)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("questions = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestListConditionMismatch: a rule whose condition and EARS text
// disagree asks which is right, with the disagreement as detail.
func TestListConditionMismatch(t *testing.T) {
	cases := map[string]struct {
		disagreement string
		want         []string
	}{
		"disagree": {
			"text 30 (not cited)",
			[]string{questions.KindConditionMismatch + " text 30 (not cited)"},
		},
		"agree": {"", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			extra := map[string]string{"nature": "business"}
			if tc.disagreement != "" {
				extra[graph.ExtraConditionDisagreement] = tc.disagreement
			}
			var got []string
			for _, q := range questions.List(
				[]graph.Node{{ID: ruleID, Type: graph.TypeRule, Extra: extra}}, nil, nil) {
				got = append(got, q.Kind+" "+q.Detail)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("questions = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestListStaleTarget: a target a person set closes the nfr_target
// question; set for code that changed since, it asks whether it still
// holds.
func TestListStaleTarget(t *testing.T) {
	cases := map[string]struct {
		extra map[string]string
		want  []string
	}{
		"no target": {nil, []string{questions.KindNFRTarget + " " + performance}},
		"set":       {map[string]string{graph.ExtraThreshold: "must 200 ms"}, nil},
		"set for other code": {
			map[string]string{graph.ExtraThreshold: "must 200 ms", graph.ExtraTargetStale: "d1"},
			[]string{questions.KindStaleTarget + " d1"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			extra := map[string]string{
				nature: technical, graph.ExtraQualityAttribute: performance,
				graph.ExtraQuantity: graph.QuantityStated,
			}
			maps.Copy(extra, tc.extra)
			var got []string
			for _, q := range questions.List(
				[]graph.Node{{ID: ruleID, Type: graph.TypeRule, Extra: extra}}, nil, nil) {
				got = append(got, q.Kind+" "+q.Detail)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("questions = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestListNatureMismatch: a rule whose layer contradicts its nature asks
// only when its own evidence backs the layer; evidence backing the text,
// or none, leaves the disagreement recorded but not asked.
func TestListNatureMismatch(t *testing.T) {
	cases := map[string]struct {
		evidence string
		want     []string
	}{
		"evidence backs the layer": {
			evidence: "business: touches entity.invoices; technical: quality_attribute reliability",
			want: []string{"nature_mismatch technical by position, business by text; " +
				"business: touches entity.invoices; technical: quality_attribute reliability"},
		},
		"evidence backs the text": {evidence: "business: touches entity.invoices"},
		"no evidence":             {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			extra := map[string]string{nature: business, graph.ExtraNatureByPosition: technical}
			if tc.evidence != "" {
				extra[graph.ExtraNatureEvidence] = tc.evidence
			}
			var got []string
			for _, q := range questions.List(
				[]graph.Node{{ID: ruleID, Type: graph.TypeRule, Extra: extra}}, nil, nil) {
				got = append(got, q.Kind+" "+q.Detail)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("questions = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestListConceptConflict: a rule naming concepts with no common owner is
// a question listing each with its evidence; rewording the evidence keeps
// its id, another option opens a new one.
func TestListConceptConflict(t *testing.T) {
	ask := func(options string) questions.Question {
		t.Helper()
		got := questions.List([]graph.Node{{
			ID: ruleID, Type: graph.TypeRule,
			Extra: map[string]string{concept.ExtraConceptConflict: options},
		}}, nil, nil)
		if len(got) != 1 || got[0].Kind != questions.KindConceptConflict {
			t.Fatalf("questions = %+v, want one concept_conflict", got)
		}
		return got[0]
	}
	first := ask("a.Config: touches entity.configs; c.Classroom: touches entity.classrooms")
	if first.Detail != "a.Config: touches entity.configs; c.Classroom: touches entity.classrooms" {
		t.Errorf("detail = %q", first.Detail)
	}
	reworded := ask(
		"a.Config: condition configs.x, touches entity.configs; c.Classroom: touches entity.classrooms",
	)
	if reworded.ID != first.ID {
		t.Error("new evidence for the same options opened another question")
	}
	if other := ask(
		"a.Config: touches entity.configs; u.User: touches entity.users",
	); other.ID == first.ID {
		t.Error("other options kept the question's id")
	}
}

// TestListConceptDivergence: a divergent pair is a question quoting three
// evidence lines and counting the rest; more evidence keeps its id; a
// converging pair asks nothing; a resolution hides it.
func TestListConceptDivergence(t *testing.T) {
	pair := func(outcome alignment.Outcome, evidence ...string) alignment.Pair {
		return alignment.Pair{A: "m.A", B: "m.B", Outcome: outcome, Evidence: evidence}
	}
	cases := map[string]struct {
		pair       alignment.Pair
		wantDetail string // "" for no question
	}{
		"diverges, one line": {
			pair(alignment.Diverges, "m.A → m.B: call a.go:1-2 → b.go:3-4"),
			"m.A → m.B: call a.go:1-2 → b.go:3-4",
		},
		"diverges, five lines": {
			pair(alignment.Diverges, "e1", "e2", "e3", "e4", "e5"),
			"e1; e2; e3; +2",
		},
		"converges": {pair(alignment.Converges, "e1"), ""},
		"absent":    {pair(alignment.Absent), ""},
	}
	ids := map[string]bool{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := questions.List(nil, nil, []alignment.Pair{tc.pair})
			if tc.wantDetail == "" {
				if len(got) != 0 {
					t.Fatalf("questions = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0].Kind != questions.KindConceptDivergence ||
				got[0].Subject != "m.A ~ m.B" || got[0].Detail != tc.wantDetail {
				t.Fatalf("questions = %+v, want one divergence %q", got, tc.wantDetail)
			}
			ids[got[0].ID] = true
			ledger := questions.Apply(got, map[string]questions.Resolution{
				got[0].ID: {Disposition: questions.Verified, Reason: "the model lacks it"},
			})
			if !ledger.ResolvedDivergence(&tc.pair) {
				t.Error("resolved divergence not reported resolved")
			}
		})
	}
	if len(ids) != 1 {
		t.Errorf("ids = %v, want one: more evidence keeps the question", ids)
	}
}
