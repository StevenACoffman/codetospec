package drift_test

import (
	"slices"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Files and spans the callee tests share: a handler and the helper it calls.
const (
	handlerFile  = "h.go"
	helperFile   = "v.go"
	helperCallee = "v.go:3-9"
)

// call is a call fact: the definition at caller calls the one at callee.
func call(callerPath, callerLines, calleePath, calleeLines string) extract.Fact {
	return extract.Fact{
		Kind:   extract.KindCall,
		Source: extract.Ref{Path: callerPath, Lines: callerLines},
		Attrs: map[string]string{
			extract.AttrCalleePath:  calleePath,
			extract.AttrCalleeLines: calleeLines,
		},
	}
}

// viaInterface marks facts as uses through an interface.
func viaInterface(facts ...extract.Fact) []extract.Fact {
	for i := range facts {
		facts[i].Attrs[extract.AttrDispatch] = extract.DispatchInterface
	}
	return facts
}

// TestCallees: one hop from the cited lines, sorted, deduplicated, never
// code the rule already cites, never through a shared root or an
// interface.
func TestCallees(t *testing.T) {
	cited := []extract.Ref{{Path: handlerFile, Lines: "10-20"}}
	cases := []struct {
		name  string
		calls []extract.Fact
		want  []string
	}{
		{"no calls", nil, nil},
		{"caller overlaps the citation", []extract.Fact{
			call(handlerFile, "1-15", helperFile, "3-9"),
		}, []string{helperCallee}},
		{"caller elsewhere in the file", []extract.Fact{
			call(handlerFile, "30-40", helperFile, "3-9"),
		}, nil},
		{"sorted and deduplicated", []extract.Fact{
			call(handlerFile, "10-20", "w.go", "1-2"),
			call(handlerFile, "10-20", helperFile, "3-9"),
			call(handlerFile, "12-14", helperFile, "3-9"),
		}, []string{helperCallee, "w.go:1-2"}},
		{"callee already cited", []extract.Fact{
			call(handlerFile, "10-20", handlerFile, "18-19"),
		}, nil},
		{"not a call fact", []extract.Fact{
			{Kind: extract.KindSymbol, Source: extract.Ref{Path: handlerFile, Lines: "10-20"}},
		}, nil},
		{"a call through an interface", viaInterface(
			call(handlerFile, "10-20", helperFile, "3-9"),
		), nil},
		{"two hops are not followed", []extract.Fact{
			call(handlerFile, "10-20", helperFile, "3-9"),
			call(helperFile, "3-9", "w.go", "1-2"),
		}, []string{helperCallee}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, r := range drift.Callees(cited, tc.calls) {
				got = append(got, r.File()+":"+r.Lines)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Callees = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCheckAffected: an edited callee marks the rule affected, not stale,
// and a drift run still passes; an edited citation still wins as stale.
func TestCheckAffected(t *testing.T) {
	const (
		handler = "package x\n\nfunc h() int {\n\treturn v()\n}\n"
		helper  = "package x\n\nfunc v() int {\n\treturn 1\n}\n"
	)
	cases := []struct {
		name          string
		handler, help string // the files at check time
		want          drift.Status
	}{
		{"nothing changed", handler, helper, drift.StatusOK},
		{
			"callee changed",
			handler,
			"package x\n\nfunc v() int {\n\treturn 2\n}\n",
			drift.StatusAffected,
		},
		{"callee gone", handler, "package x\n", drift.StatusAffected},
		{
			"citation changed too", "package x\n\nfunc h() int {\n\treturn 0\n}\n",
			"package x\n\nfunc v() int {\n\treturn 2\n}\n", drift.StatusStale,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSource(t, dir, handlerFile, handler)
			writeSource(t, dir, helperFile, helper)
			rule := ruleWithDigest(t, dir, extract.Ref{Path: handlerFile, Lines: fBody})
			rule.Callees = []extract.Ref{{Path: helperFile, Lines: fBody}}
			calleesDigest, err := drift.Digest(srcRoots(dir), rule.Callees)
			if err != nil {
				t.Fatal(err)
			}
			rule.Extra[graph.ExtraCalleesDigest] = calleesDigest

			writeSource(t, dir, handlerFile, tc.handler)
			writeSource(t, dir, helperFile, tc.help)
			report := drift.Check([]graph.Node{rule}, srcRoots(dir))

			if got := report.Rules[0].Status; got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
			if wantDrifted := tc.want == drift.StatusStale; report.Drifted() != wantDrifted {
				t.Errorf("Drifted() = %v, want %v", report.Drifted(), wantDrifted)
			}
		})
	}
}
