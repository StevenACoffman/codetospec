package drift_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// calcSource is the file the moved tests stamp a rule against: f's body is
// lines 3-5.
const calcSource = "package x\n\nfunc f() int {\n\treturn 1\n}\n"

// TestCheckMoved: code that only moved (lines added or removed above it)
// is moved, not stale, and never fails the check; code that moved and
// changed is stale; a rule stamped before source hashes existed stays
// stale.
func TestCheckMoved(t *testing.T) {
	cases := map[string]struct {
		after      string
		oldStamp   bool // drop source_hashes, as an older graph has none
		want       drift.Status
		wantDetail string
	}{
		"unchanged": {after: calcSource, want: drift.StatusOK},
		"line added above": {
			after:      "package x\n\n// f is one.\n\nfunc f() int {\n\treturn 1\n}\n",
			want:       drift.StatusMoved,
			wantDetail: "calc.go 3-5 → 5-7",
		},
		"lines removed above, now past the old range end": {
			after:      "func f() int {\n\treturn 1\n}\n",
			want:       drift.StatusMoved,
			wantDetail: "calc.go 3-5 → 1-3",
		},
		"moved and changed": {
			after: "package x\n\n// f is two.\n\nfunc f() int {\n\treturn 2\n}\n",
			want:  drift.StatusStale,
		},
		"moved, older stamp": {
			after:    "package x\n\n// f is one.\n\nfunc f() int {\n\treturn 1\n}\n",
			oldStamp: true,
			want:     drift.StatusStale,
		},
		"nearest copy wins": {
			after: "func f() int {\n\treturn 1\n}\n\nfunc f() int {\n\treturn 1\n}\n" +
				"\n\n\n\n\n\n",
			want:       drift.StatusMoved,
			wantDetail: "calc.go 3-5 → 1-3",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeSource(t, dir, calcFile, calcSource)
			rule := graph.Node{
				ID: ruleID, Type: graph.TypeRule,
				Sources: []extract.Ref{{Path: calcFile, Lines: fBody}},
			}
			stamped := drift.Stamp(srcRoots(dir), []graph.Node{rule}, nil)[0]
			if tc.oldStamp {
				delete(stamped.Extra, graph.ExtraSourceHashes)
			}
			writeSource(t, dir, calcFile, tc.after)

			r := drift.Check([]graph.Node{stamped}, srcRoots(dir))

			got := r.Rules[0]
			if got.Status != tc.want || !strings.Contains(got.Detail, tc.wantDetail) {
				t.Fatalf(
					"status = %s (%s), want %s (%s)",
					got.Status,
					got.Detail,
					tc.want,
					tc.wantDetail,
				)
			}
			if wantFail := tc.want == drift.StatusStale; r.Drifted() != wantFail {
				t.Errorf("Drifted() = %v, want %v", r.Drifted(), wantFail)
			}
		})
	}
}

// TestStamp: a rule gets its digest and one text hash per source, in
// source order; the input node is left as it was.
func TestStamp(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, calcFile, calcSource)
	rule := graph.Node{
		ID: ruleID, Type: graph.TypeRule,
		Sources: []extract.Ref{{Path: calcFile, Lines: fBody}, {Path: calcFile, Lines: "1-1"}},
		Extra:   map[string]string{"ears": "ubiquitous"},
	}
	entity := graph.Node{ID: "entity.x", Type: graph.TypeEntity}

	out := drift.Stamp(srcRoots(dir), []graph.Node{rule, entity}, nil)

	if _, set := rule.Extra[graph.ExtraDigest]; set {
		t.Error("Stamp modified its input's Extra map")
	}
	if out[0].Extra[graph.ExtraDigest] == "" || out[0].Extra["ears"] != "ubiquitous" {
		t.Errorf("rule extra = %v, want a digest beside its own keys", out[0].Extra)
	}
	if hashes := strings.Split(out[0].Extra[graph.ExtraSourceHashes], ","); len(hashes) != 2 ||
		hashes[0] == hashes[1] {
		t.Errorf(
			"source_hashes = %q, want two distinct hashes",
			out[0].Extra[graph.ExtraSourceHashes],
		)
	}
	if out[1].Extra != nil {
		t.Errorf("entity extra = %v, want none", out[1].Extra)
	}
}
