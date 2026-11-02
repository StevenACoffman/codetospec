package nfrtarget_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/nfrtarget"
)

// The ids of the rules the cases set targets for.
const (
	latency = "rule.billing.latency"
	round   = "rule.billing.round"
)

// write writes content as the targets file of a new output directory.
func write(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, nfrtarget.File), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLoad: a well-formed file loads; a malformed entry fails, naming the
// entry and what is wrong; no file is no targets.
func TestLoad(t *testing.T) {
	measure := "rule.billing.latency:\n  digest: d1\n  measure:\n" +
		"    scale: p95 latency\n    meter: load test\n    must: 200 ms\n"
	cases := map[string]struct {
		content string
		want    string // "" when valid
	}{
		"a measure":     {measure, ""},
		"a policy":      {"rule.billing.audit:\n  digest: d1\n  policy: Log every change.\n", ""},
		"an example":    {"rule.billing.ui:\n  digest: d1\n  example: the 2026 checkout\n", ""},
		"empty":         {"", ""},
		"not a rule id": {"q-0123456789ab:\n  digest: d1\n  policy: x\n", "not a rule id"},
		"no digest":     {"rule.billing.audit:\n  policy: x\n", "digest"},
		"two answers": {
			"rule.billing.audit:\n  digest: d1\n  policy: x\n  example: y\n",
			"exactly one",
		},
		"no answer": {"rule.billing.audit:\n  digest: d1\n", "exactly one"},
		"a measure, no meter": {
			strings.Replace(measure, "    meter: load test\n", "", 1), "scale and a meter",
		},
		"a measure, no level": {
			strings.Replace(measure, "    must: 200 ms\n", "", 1), "must or a plan",
		},
		"an unknown field": {"rule.billing.audit:\n  digest: d1\n  polcy: x\n", "polcy"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := nfrtarget.Load(write(t, tc.content))
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("Load: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("Load error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	if got, err := nfrtarget.Load(t.TempDir()); err != nil || len(got) != 0 {
		t.Errorf("no file: Load = %v, %v, want no targets", got, err)
	}
}

// nfr is a non-functional rule whose cited code has digest.
func nfr(id, digest string) graph.Node {
	return graph.Node{ID: id, Type: graph.TypeRule, Extra: map[string]string{
		"nature": "technical", graph.ExtraQualityAttribute: "performance_efficiency",
		graph.ExtraDigest: digest, graph.ExtraThreshold: "30",
	}}
}

// TestAnnotate: a target sets its rule's threshold and kind, over the
// stated threshold; one set for other code is marked stale; a target
// without a non-functional rule is unused; the input is not modified.
func TestAnnotate(t *testing.T) {
	in := []graph.Node{
		nfr(latency, "d2"),
		nfr("rule.billing.audit", "d3"),
		{
			ID:    "rule.billing.round",
			Type:  graph.TypeRule,
			Extra: map[string]string{"nature": "business"},
		},
		nfr("rule.billing.untouched", "d4"),
	}
	set := map[string]nfrtarget.Target{
		latency: {Digest: "d1", Measure: &nfrtarget.Measure{
			Scale: "p95 latency", Meter: "load test", Must: "200 ms", Plan: "120 ms",
		}},
		"rule.billing.audit": {Digest: "d3", Policy: "Log every change."},
		round:                {Digest: "d1", Example: "x"},
		"rule.billing.gone":  {Digest: "d1", Example: "y"},
	}
	out, unused := nfrtarget.Annotate(in, set)
	cases := map[string]struct {
		node                        graph.Node
		threshold, kind, staleSince string
	}{
		"measure, code changed": {out[0], "must 200 ms; plan 120 ms", "measure", "d1"},
		"policy, same code":     {out[1], "Log every change.", "policy", ""},
		"functional rule":       {out[2], "", "", ""},
		"no target":             {out[3], "30", "", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := tc.node.Extra
			if e[graph.ExtraThreshold] != tc.threshold || e[graph.ExtraTarget] != tc.kind ||
				e[graph.ExtraTargetStale] != tc.staleSince {
				t.Errorf("extra = %v, want threshold %q, target %q, stale %q",
					e, tc.threshold, tc.kind, tc.staleSince)
			}
		})
	}
	if want := []string{"rule.billing.gone", round}; !slices.Equal(unused, want) {
		t.Errorf("unused = %v, want %v", unused, want)
	}
	if in[0].Extra[graph.ExtraThreshold] != "30" {
		t.Error("the input node was modified")
	}
}
