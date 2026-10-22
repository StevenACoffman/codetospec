package main

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/ui"
)

// loadPrior reads the previous output and checks that each rule a person
// owns still cites code that exists: it would fail verification after the
// LLM phases otherwise. A node file other than a rule's that was edited is
// rewritten by this run: the warning says so.
func (p *pipeline) loadPrior() (render.Prior, error) {
	prior, err := render.LoadPrior(p.cfg.Out)
	if err != nil {
		return render.Prior{}, fmt.Errorf("read the previous graph: %w", err)
	}
	roots := p.cfg.Roots()
	for _, id := range slices.Sorted(maps.Keys(prior.Reviewed)) {
		n := prior.Reviewed[id].Node
		if _, digestErr := drift.Digest(roots, n.Sources); digestErr != nil {
			return render.Prior{}, fmt.Errorf(
				"reviewed rule %s no longer cites existing code (%w): fix or delete %s",
				id, digestErr, render.NodeFile(&n))
		}
	}
	for _, file := range prior.Overwritten {
		p.warn("edited by hand, overwritten (only rule files are kept): " + file)
	}
	return prior, nil
}

// keepReviewed keeps the rules a person owns in place of the generated
// ones (see render.KeepReviewed), warning about each kept rule whose cited
// code changed since and each one this run no longer produces.
func (p *pipeline) keepReviewed(
	nodes []graph.Node,
	reviewed map[string]render.Reviewed,
) ([]graph.Node, map[string][]byte) {
	nodes, files, dropped := render.KeepReviewed(nodes, reviewed)
	keptRules := make([]graph.Node, 0, len(files))
	for _, n := range nodes {
		if _, kept := files[n.ID]; kept {
			keptRules = append(keptRules, n)
		}
	}
	for _, rs := range drift.Check(keptRules, p.cfg.Roots()).Rules {
		switch rs.Status {
		case drift.StatusStale:
			p.warn("reviewed rule " + rs.RuleID + " kept, but its cited code changed: review it")
		case drift.StatusMoved:
			p.warn("reviewed rule " + rs.RuleID + " kept, but its cited code moved: " + rs.Detail)
		case drift.StatusOK, drift.StatusAffected, drift.StatusUnknown, drift.StatusBroken:
			// Nothing to review; a broken citation was refused by loadPrior.
		}
	}
	for _, id := range dropped {
		p.warn("reviewed rule " + id + " is no longer produced: dropped")
	}
	return nodes, files
}

// warn reports a degraded outcome the run continues past.
func (p *pipeline) warn(message string) {
	p.sink.Emit(ui.LogLine{Level: slog.LevelWarn, Message: message})
}
