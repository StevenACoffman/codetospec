// Package corpus checks rendered spec graphs of real repositories against
// recorded baselines, without an LLM: the deterministic phases (grounding,
// verify, drift) re-run over code the fixture cannot represent, and any
// count that gets worse is a regression.
//
// The graphs and their sources live outside this repository (a scan of a
// private monorepo), so the check is a build-tagged test that skips what
// it cannot find; see corpus_test.go.
package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/ears"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Manifest lists the graphs to check.
type Manifest struct {
	Entries []Entry `json:"entries"`
}

// Entry is one rendered graph and the source tree it was generated from.
type Entry struct {
	Name string `json:"name"`
	// Src is the analyzed tree and Out the rendered graph; a leading "~/"
	// is the user's home directory.
	Src string `json:"src"`
	Out string `json:"out"`
	// RefRoots are the --ref-root trees the graph's citations name.
	RefRoots map[string]string `json:"ref_roots,omitempty"`
	// Lang is the graph's --lang: it picks the decimal separator grounding
	// reads numbers with.
	Lang string `json:"lang"`
	// Commit is the HEAD of Src when Baseline was recorded: on another
	// commit the cited code may differ, so the entry is not comparable.
	Commit   string   `json:"commit"`
	Baseline Measures `json:"baseline"`
}

// Measures are the counts a corpus run compares.
type Measures struct {
	Rules      int `json:"rules"`
	Grounded   int `json:"grounded"`
	Ungrounded int `json:"ungrounded"`
	// Proven counts rules whose crosscheck verdict is supported, repaired
	// or mechanical and that are grounded: the rules an agent may rely on.
	Proven         int `json:"proven"`
	PartialDomains int `json:"partial_domains"`
	Violations     int `json:"violations"` // verify findings
	Drifted        int `json:"drifted"`    // stale or broken rules
	// EARSIssues counts rules whose requirement does not have the form of
	// their EARS kind, by the current ears.Check (old graphs carry no
	// ears_issue, so it is recomputed).
	EARSIssues int `json:"ears_issues"`
}

// LoadManifest reads a manifest file.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Manifest{}, fmt.Errorf("read corpus manifest: %w", err)
	}
	var m Manifest
	if err = json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse corpus manifest %s: %w", path, err)
	}
	return m, nil
}

// Measure counts what a graph's nodes, re-checked by the current code,
// report: nodes after grounding.Annotate, the verify violation count and
// the drift report of the same tree.
func Measure(nodes []graph.Node, violations int, report drift.Report) Measures {
	m := Measures{Violations: violations, Drifted: report.Stale + report.Broken}
	for i := range nodes {
		n := &nodes[i]
		switch n.Type {
		case graph.TypeDomain:
			if n.Extra[graph.ExtraCoverage] == graph.CoveragePartial {
				m.PartialDomains++
			}
		case graph.TypeRule:
			m.Rules++
			if ears.Check(n.Extra["ears"], graph.RuleRequirement(n.Body)) != "" {
				m.EARSIssues++
			}
			switch n.Extra["grounding"] {
			case "grounded":
				m.Grounded++
				if trusted(n.Extra["crosscheck"]) {
					m.Proven++
				}
			case "ungrounded":
				m.Ungrounded++
			}
		}
	}
	return m
}

// trusted reports whether a crosscheck verdict lets an agent rely on a
// grounded rule.
func trusted(verdict string) bool {
	switch verdict {
	case "supported", "repaired", "mechanical":
		return true
	}
	return false
}

// Compare lists how got is worse than baseline; nil when it is not. A
// changed rule count is listed too: the graph is not the one measured.
func Compare(baseline, got Measures) []string {
	var worse []string
	more := func(name string, b, g int) {
		if g > b {
			worse = append(worse, fmt.Sprintf("%s: %d, baseline %d", name, g, b))
		}
	}
	fewer := func(name string, b, g int) {
		if g < b {
			worse = append(worse, fmt.Sprintf("%s: %d, baseline %d", name, g, b))
		}
	}
	if got.Rules != baseline.Rules {
		worse = append(worse, fmt.Sprintf(
			"rules: %d, baseline %d (another graph: re-record the baseline)",
			got.Rules, baseline.Rules))
	}
	fewer("grounded", baseline.Grounded, got.Grounded)
	more("ungrounded", baseline.Ungrounded, got.Ungrounded)
	fewer("proven", baseline.Proven, got.Proven)
	more("partial domains", baseline.PartialDomains, got.PartialDomains)
	more("verify violations", baseline.Violations, got.Violations)
	more("drifted rules", baseline.Drifted, got.Drifted)
	more("EARS form issues", baseline.EARSIssues, got.EARSIssues)
	return worse
}
