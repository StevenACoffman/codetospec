package questions_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	qs := questions.List(nodes, nil)
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
	for _, q := range questions.List(nodes, gaps) {
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
	ledger := questions.Apply(questions.List(nodes, nil), map[string]questions.Resolution{
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
	listed := questions.List(nil, gaps)
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
