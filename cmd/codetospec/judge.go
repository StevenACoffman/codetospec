package main

import (
	"context"
	"path/filepath"

	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/quality"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/state"
	"github.com/StevenACoffman/codetospec/internal/testreach"
	"github.com/StevenACoffman/codetospec/internal/ui"
)

// judged is the built graph after judge.
type judged struct {
	nodes         []graph.Node
	tally         crosscheck.Tally
	reviewedFiles map[string][]byte // the rule files a person owns, kept as they are
}

// judge runs the passes over the built graph: the LLM crosscheck and
// quality passes (each when enabled), the mechanical stamps (grounding,
// digests, test reach), then puts back the rules a person owns.
func (p *pipeline) judge(
	ctx context.Context,
	nodes []graph.Node,
	facts []extract.Fact,
	idx *factIndex,
	groundingOpts grounding.Options,
	prior *render.Prior,
) (judged, error) {
	nodes, tally, err := p.crosscheck(ctx, nodes, idx.symbols)
	if err != nil {
		return judged{}, err
	}
	if nodes, err = p.quality(ctx, nodes); err != nil {
		return judged{}, err
	}
	nodes = stamp(p.cfg.Roots(), nodes, facts, groundingOpts)
	nodes = testreach.Annotate(nodes, facts)
	nodes, reviewedFiles := p.keepReviewed(nodes, prior.Reviewed)
	if p.cfg.Crosscheck && len(reviewedFiles) > 0 {
		tally = crosscheck.Count(nodes) // the kept rules' verdicts, not the replaced ones'
	}
	return judged{nodes: nodes, tally: tally, reviewedFiles: reviewedFiles}, nil
}

// crosscheck runs the adversarial review when --crosscheck is set, and
// returns the nodes annotated with their verdicts and the tally; without
// it, the nodes unchanged and an empty tally.
func (p *pipeline) crosscheck(
	ctx context.Context,
	nodes []graph.Node,
	symbols map[string][]mapper.SymbolContext,
) ([]graph.Node, crosscheck.Tally, error) {
	if !p.cfg.Crosscheck {
		return nodes, crosscheck.Tally{}, nil
	}
	if err := p.enter("crosscheck", nil); err != nil {
		return nil, crosscheck.Tally{}, err
	}
	ruleCount := 0
	for i := range nodes {
		if nodes[i].Type == graph.TypeRule {
			ruleCount++
		}
	}
	checkDone := 0
	checker := &crosscheck.Checker{
		Log:        p.logger,
		LLM:        p.client,
		Model:      p.cfg.Model,
		Lang:       p.cfg.Lang,
		Workers:    p.cfg.Workers,
		OutDir:     filepath.Join(p.cfg.Out, ".codetospec", "crosscheck"),
		SrcRoot:    p.cfg.Src,
		Repair:     p.cfg.Repair,
		SymbolsFor: func(path string) []mapper.SymbolContext { return symbols[path] },
		OnUnit: func(v crosscheck.Verdict, usage llm.Usage) {
			p.record(ctx, func(s *state.State) {
				s.AddTokens("crosscheck", usage.PromptTokens, usage.CompletionTokens)
			})
			checkDone++
			p.sink.Emit(ui.CrosscheckUnit{
				RuleID:  v.RuleID,
				Verdict: v.Verdict,
				Done:    checkDone,
				Total:   ruleCount,
				Usage:   usage,
			})
		},
	}
	verdicts, err := checker.Run(ctx, nodes)
	if err != nil {
		return nil, crosscheck.Tally{}, err
	}
	nodes = crosscheck.Annotate(nodes, verdicts)
	return nodes, crosscheck.Count(nodes), nil
}

// stamp checks, without an LLM, that each rule's stated values occur in
// the code it cites (after crosscheck, whose repair may re-cite), and
// stamps each rule with a digest of that code, and of the code it calls
// (from facts' call facts), for later drift checks.
func stamp(
	roots extract.Roots,
	nodes []graph.Node,
	facts []extract.Fact,
	opts grounding.Options,
) []graph.Node {
	return drift.Stamp(roots, grounding.Annotate(roots, nodes, opts), facts)
}

// quality classifies the technical rules by quality attribute when
// --quality is set; without it, the nodes unchanged.
func (p *pipeline) quality(ctx context.Context, nodes []graph.Node) ([]graph.Node, error) {
	if !p.cfg.Quality {
		return nodes, nil
	}
	if err := p.enter("quality", nil); err != nil {
		return nil, err
	}
	classifier := &quality.Classifier{
		LLM:       p.client,
		Log:       p.logger,
		Model:     p.cfg.Model,
		Workers:   p.cfg.Workers,
		CachePath: filepath.Join(p.cfg.Out, ".codetospec", "quality.json"),
		OnBatch: func(usage llm.Usage) {
			p.record(ctx, func(s *state.State) {
				s.AddTokens("quality", usage.PromptTokens, usage.CompletionTokens)
			})
		},
	}
	attributes, err := classifier.Run(ctx, nodes)
	if err != nil {
		return nil, err
	}
	return quality.Annotate(nodes, attributes), nil
}
