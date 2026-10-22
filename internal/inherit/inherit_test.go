package inherit_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/inherit"
)

// rootName is the shared root the tests inherit from.
const rootName = "pkg"

// supported is the shared spec's verdict on its rules.
const supported = "supported"

// specRule is a rule of the shared spec citing path:lines.
func specRule(id, path, lines string) graph.Node {
	return graph.Node{
		ID:      id,
		Type:    graph.TypeRule,
		Title:   "Roster sync skips archived classes",
		Body:    "**Requirement (EARS)**: IF a class is archived, THEN the system shall skip it.",
		Sources: []extract.Ref{{Path: path, Lines: lines}},
		Extra: map[string]string{
			"ears": "unwanted", "nature": "business", "crosscheck": supported,
			"origin": "explicit", "confidence": "0.90",
		},
	}
}

// syncFile is the shared file the tests cite; sharedTree holds it.
const syncFile = "roster/sync.go"

// sharedTree is the shared root's code: 30 lines of roster/sync.go.
func sharedTree() fstest.MapFS {
	return fstest.MapFS{syncFile: {Data: []byte(strings.Repeat("line\n", 30))}}
}

// reachOf is a reach fact of the shared root citing path:lines.
func reachOf(path, lines string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindReach, ID: "reach." + path + lines,
		Source: extract.Ref{Path: path, Lines: lines, Root: rootName},
	}
}

func TestRulesInheritsReachedRules(t *testing.T) {
	spec := []graph.Node{specRule("rule.roster.skip-archived", "roster/sync.go", "10-20")}
	got := inherit.Rules(
		spec,
		rootName,
		[]extract.Fact{reachOf("roster/sync.go", "5-30")},
		sharedTree(),
	)
	want := []extract.Fact{{
		Kind: extract.KindRule,
		ID:   "rule.inherited.pkg.roster.skip-archived",
		Attrs: map[string]string{
			extract.AttrSlug:          "skip-archived",
			extract.AttrTitle:         "Roster sync skips archived classes",
			extract.AttrRequirement:   "IF a class is archived, THEN the system shall skip it.",
			extract.AttrEARS:          "unwanted",
			extract.AttrNature:        "business",
			extract.AttrDomain:        "shared-roster",
			extract.AttrInheritedFrom: rootName,
			extract.AttrCrosscheck:    supported,
			extract.AttrOrigin:        "explicit",
			extract.AttrConfidence:    "0.90",
		},
		Source:    extract.Ref{Path: "roster/sync.go", Lines: "10-20", Root: rootName},
		Origin:    "inherit",
		Certainty: "static",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// TestRulesSkips: what is not reached, or not the spec's own single-citation
// LLM rule, is not inherited.
func TestRulesSkips(t *testing.T) {
	reached := []extract.Fact{reachOf("roster/sync.go", "5-30")}
	mechanical := specRule("rule.roster.mech", "roster/sync.go", "10-20")
	mechanical.Extra[graph.ExtraExtractedBy] = "go"
	inherited := specRule("rule.roster.again", "roster/sync.go", "10-20")
	inherited.Extra[graph.ExtraInheritedFrom] = "other"
	twoCitations := specRule("rule.roster.two", "roster/sync.go", "10-20")
	twoCitations.Sources = append(
		twoCitations.Sources,
		extract.Ref{Path: "roster/x.go", Lines: "1-2"},
	)
	cases := map[string]struct {
		rule  graph.Node
		reach []extract.Fact
	}{
		"citation outside reached lines": {
			specRule("rule.roster.far", "roster/sync.go", "40-50"), reached,
		},
		"citation in an unreached file": {
			specRule("rule.roster.other", "roster/other.go", "10-20"), reached,
		},
		"reach of another root": {
			specRule("rule.roster.root", "roster/sync.go", "10-20"),
			[]extract.Fact{{
				Kind:   extract.KindReach,
				Source: extract.Ref{Path: "roster/sync.go", Lines: "1-99", Root: "webapp"},
			}},
		},
		"mechanical rule":           {mechanical, reached},
		"rule inherited elsewhere":  {inherited, reached},
		"rule citing several spans": {twoCitations, reached},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := inherit.Rules(
				[]graph.Node{tc.rule},
				rootName,
				tc.reach,
				sharedTree(),
			); len(
				got,
			) != 0 {
				t.Fatalf("inherited %+v", got)
			}
		})
	}
}

// TestRulesStaleVerdict: an inherited rule keeps the shared spec's verdict
// while its cited code has the digest the spec stored; once that code
// changed, the rule is unchecked and the old verdict is kept as stale.
func TestRulesStaleVerdict(t *testing.T) {
	reached := []extract.Fact{reachOf(syncFile, "5-30")}
	src := extract.Ref{Path: syncFile, Lines: "10-20"}
	current, err := drift.Digest(extract.Roots{Src: sharedTree()}, []extract.Ref{src})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		digest, wantVerdict, wantStale string
	}{
		"code unchanged": {digest: current, wantVerdict: supported},
		"code changed": {
			digest: "0123456789abcdef", wantVerdict: "unchecked", wantStale: supported,
		},
		"no digest (older spec)": {wantVerdict: supported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rule := specRule("rule.roster.skip-archived", syncFile, "10-20")
			if tc.digest != "" {
				rule.Extra[graph.ExtraDigest] = tc.digest
			}
			got := inherit.Rules([]graph.Node{rule}, rootName, reached, sharedTree())
			if len(got) != 1 {
				t.Fatalf("inherited %d rules, want 1", len(got))
			}
			a := got[0].Attrs
			if a[extract.AttrCrosscheck] != tc.wantVerdict ||
				a[extract.AttrStaleVerdict] != tc.wantStale {
				t.Errorf(
					"crosscheck %q, stale %q; want %q, %q",
					a[extract.AttrCrosscheck],
					a[extract.AttrStaleVerdict],
					tc.wantVerdict,
					tc.wantStale,
				)
			}
		})
	}
}
