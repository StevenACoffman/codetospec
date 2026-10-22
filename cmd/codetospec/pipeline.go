package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/callers"
	"github.com/StevenACoffman/codetospec/internal/communities"
	"github.com/StevenACoffman/codetospec/internal/config"
	"github.com/StevenACoffman/codetospec/internal/consistency"
	"github.com/StevenACoffman/codetospec/internal/crosscheck"
	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/drift"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/grounding"
	"github.com/StevenACoffman/codetospec/internal/inherit"
	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/mapper"
	"github.com/StevenACoffman/codetospec/internal/reducer"
	"github.com/StevenACoffman/codetospec/internal/render"
	"github.com/StevenACoffman/codetospec/internal/sitter"
	"github.com/StevenACoffman/codetospec/internal/source"
	"github.com/StevenACoffman/codetospec/internal/state"
	"github.com/StevenACoffman/codetospec/internal/ui"
	"github.com/StevenACoffman/codetospec/internal/verify"
)

// runPipeline is the full run sequence: extract, chunk, map, reduce, build,
// crosscheck, verify, render. Progress is reported through the sink, which
// also receives a final RunFinished event (consumed by the TUI and the plain
// summary), whether the run succeeds or not.
func runPipeline(
	ctx context.Context,
	cfg *config.Config,
	sink ui.Sink,
	logger *slog.Logger,
) error {
	sink.Emit(ui.RunStarted{Src: cfg.Src, Out: cfg.Out, Model: cfg.Model, Workers: cfg.Workers})
	p := &pipeline{
		cfg:    cfg,
		sink:   sink,
		logger: logger,
		final:  ui.RunFinished{Extractors: map[string]string{}},
	}
	err := p.run(ctx)
	p.final.Err = err
	sink.Emit(p.final)
	return err
}

// pipeline is one run's shared context: what every phase reports to, and
// the RunFinished it fills in. Each phase is a method returning its result;
// computing what needs no I/O is left to pure functions.
type pipeline struct {
	cfg    *config.Config
	sink   ui.Sink
	logger *slog.Logger
	st     *state.Store // set once the facts are written
	client llm.Chatter  // set before the first LLM phase
	final  ui.RunFinished
}

// extraction is what the extract phase knows of the source tree.
type extraction struct {
	files     []source.File
	fileData  map[string][]byte
	fileInfo  map[string]*sitter.FileInfo
	noGrammar []string
	facts     []extract.Fact
	rejected  []string // malformed rule facts, with their reason
	status    map[string]string
}

// run is the phase sequence; runPipeline reports its outcome.
func (p *pipeline) run(ctx context.Context) error {
	cfg := p.cfg
	// Human domain models, linted first: an invalid one fails the run
	// before any LLM call.
	var (
		models *domainmodel.Set
		err    error
	)
	if len(cfg.DomainModels) > 0 {
		if models, err = domainmodel.Load(cfg.DomainModels); err != nil {
			return err
		}
	}

	ext, err := p.start(ctx)
	if err != nil {
		return err
	}

	// A domain fact from an extractor wins; otherwise the configured
	// strategy (namespace or path) decides.
	resolver := extract.DomainResolver{
		Strategy:  cfg.DomainStrategy,
		Depth:     cfg.DomainDepth,
		Ambiguous: extract.AmbiguousNamespaces(ext.fileInfo),
	}
	hints := extract.NewDomainHints(ext.facts)
	domainOf := func(namespace, path string, line int) string {
		if domain := hints.At(path, line); domain != "" {
			return domain
		}
		return resolver.Resolve(namespace, path)
	}
	chunks, generated := chunkFiles(&ext, domainOf)
	// A definition its callers of one domain alone use joins that domain.
	chunks = callers.Reassign(chunks, ext.facts, func(path string, line int) bool {
		return hints.At(path, line) != ""
	})
	p.sink.Emit(ui.Chunked{Chunks: len(chunks)})

	idx := indexFacts(ext.facts)
	groundingOpts := grounding.Options{
		DecimalSeparator: locale.For(cfg.Lang).DecimalSeparator,
		Constants:        grounding.Constants(ext.facts),
	}
	p.client = llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.MaxTokens)
	mapOutputs, err := p.mapChunks(ctx, chunks, &idx, groundingOpts)
	if err != nil {
		return err
	}
	reduced, err := p.reduce(ctx, candidatesByDomain(mapOutputs), &idx)
	if err != nil {
		return err
	}

	if err = p.enter("build", nil); err != nil {
		return err
	}
	gaps := coverageGaps(generated, mapOutputs, reduced)
	nodes := graph.AnnotateCoverage(graph.Build(ext.facts, reduced, resolver, cfg.Lang), gaps)
	nodes, tally, err := p.crosscheck(ctx, nodes, idx.symbols)
	if err != nil {
		return err
	}
	nodes = stamp(cfg.Roots(), nodes, ext.facts, groundingOpts)
	if err = p.verify(ctx, nodes); err != nil {
		return err
	}

	snapshot := p.st.Snapshot()
	meta := buildMeta(cfg, &ext, &metaInputs{
		chunks:   chunks,
		gaps:     gaps,
		groups:   domainGroups(chunks, resolver.Ambiguous, &hints),
		nodes:    nodes,
		tally:    tally,
		snapshot: &snapshot,
		models:   models,
	})
	written, err := p.domainModel(models, nodes, tablesOf(ext.facts, domainOf))
	if err != nil {
		return err
	}
	return p.finish(nodes, &meta, &written, &snapshot)
}

// finish renders the graph and the domain models, closes the state and
// completes the run summary.
func (p *pipeline) finish(
	nodes []graph.Node,
	meta *render.Meta,
	written *domainmodel.Enhanced,
	snapshot *state.State,
) error {
	for _, f := range written.Files {
		meta.ModelFiles = append(meta.ModelFiles, f.Path)
	}
	meta.ModelUndefined = written.Undefined
	p.sink.Emit(ui.PhaseChanged{Phase: "render"})
	if err := render.Write(p.cfg.Out, nodes, meta); err != nil {
		return err
	}
	if err := p.writeModels(written); err != nil {
		return err
	}
	if err := p.st.Update(func(s *state.State) { s.Phase = "done" }); err != nil {
		return err
	}
	p.summarize(nodes, meta.Coverage, snapshot)
	return nil
}

// start opens the state store, first: an output holding another source's
// spec stops the run before anything is written. It then extracts the
// facts, persists them and records the extractors' outcome.
func (p *pipeline) start(ctx context.Context) (extraction, error) {
	statePath := filepath.Join(p.cfg.Out, ".codetospec", "state.json")
	var err error
	if p.st, err = state.Open(statePath, p.cfg.Src); err != nil {
		return extraction{}, err
	}
	ext, err := p.extract(ctx)
	if err != nil {
		return extraction{}, err
	}
	if err = writeFactsFile(p.cfg.Out, ext.facts); err != nil {
		return extraction{}, err
	}
	err = p.st.Update(func(s *state.State) { maps.Copy(s.Extractors, ext.status) })
	return ext, err
}

// enter announces phase and records it in the state store, applying reset
// (counters for the phase) when given.
func (p *pipeline) enter(phase string, reset func(*state.State)) error {
	p.sink.Emit(ui.PhaseChanged{Phase: phase})
	return p.st.Update(func(s *state.State) {
		s.Phase = phase
		if reset != nil {
			reset(s)
		}
	})
}

// extract runs the three fact layers: tree-sitter over every file (A),
// the configured external extractors (B) and the facts files (C), then
// merges them and drops malformed rule facts.
func (p *pipeline) extract(ctx context.Context) (extraction, error) {
	specs, err := loadSpecs(p.cfg.Inherit)
	if err != nil {
		return extraction{}, err
	}
	files, err := source.Walk(p.cfg.Src, p.cfg.Exclude)
	if err != nil {
		return extraction{}, err
	}
	ext := extraction{files: files}
	sitterFacts := p.readSources(ctx, &ext)
	extractorFacts, status, err := p.runExtractors(ctx)
	if err != nil {
		return extraction{}, err
	}
	ext.status = status
	p.final.Extractors = status
	fileFacts, err := loadFactsFiles(p.cfg.FactsFiles)
	if err != nil {
		return extraction{}, err
	}

	merged := extract.Merge(sitterFacts, extractorFacts, fileFacts)
	ext.facts, ext.rejected = graph.ValidateRuleFacts(
		extract.Merge(merged, inheritedRules(specs, merged)),
	)
	if err = p.cfg.Roots().CheckRoots(ext.facts); err != nil {
		return extraction{}, err
	}
	for _, reason := range ext.rejected {
		p.sink.Emit(
			ui.LogLine{Level: slog.LevelWarn, Message: "invalid rule fact dropped: " + reason},
		)
	}
	byKind := map[string]int{}
	for _, f := range ext.facts {
		byKind[f.Kind]++
	}
	p.sink.Emit(ui.FactsMerged{Total: len(ext.facts), ByKind: byKind})
	return ext, nil
}

// readSources reads and parses every walked file (layer A), filling ext's
// file data, parse trees and files without a grammar, and returns the
// universal facts.
func (p *pipeline) readSources(ctx context.Context, ext *extraction) []extract.Fact {
	var facts []extract.Fact
	ext.fileData = make(map[string][]byte, len(ext.files))
	ext.fileInfo = make(map[string]*sitter.FileInfo)
	parser := sitter.NewParser()
	for _, f := range ext.files {
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			p.logger.WarnContext(ctx, "unreadable file skipped", "path", f.Path, "err", err)
			continue
		}
		ext.fileData[f.Path] = data
		if f.Language == "" {
			ext.noGrammar = append(ext.noGrammar, f.Path)
			p.sink.Emit(ui.FileExtracted{Path: f.Path})
			continue
		}
		info, err := parser.Parse(f.Language, data)
		if err != nil {
			p.logger.WarnContext(ctx, "parse failed, falling back to line chunks",
				"path", f.Path, "err", err)
			ext.noGrammar = append(ext.noGrammar, f.Path)
			p.sink.Emit(ui.FileExtracted{Path: f.Path, Language: f.Language})
			continue
		}
		ext.fileInfo[f.Path] = info
		fileFacts := extract.UniversalFacts(f.Path, info)
		facts = append(facts, fileFacts...)
		p.sink.Emit(ui.FileExtracted{Path: f.Path, Language: f.Language, Facts: len(fileFacts)})
	}
	p.logger.InfoContext(ctx, "extract: universal layer done",
		"files", len(ext.files), "facts", len(facts))
	return facts
}

// runExtractors runs the configured external extractors (layer B). A
// failing extractor is degraded to a warning and a "failed" status; only
// cancellation stops the run.
func (p *pipeline) runExtractors(ctx context.Context) ([]extract.Fact, map[string]string, error) {
	status := map[string]string{}
	var facts []extract.Fact
	for _, e := range p.cfg.Extractors {
		timeout, err := e.TimeoutDuration()
		if err != nil {
			return nil, nil, err
		}
		extracted, err := extract.RunExtractor(ctx, p.logger, extract.ExtractorConfig{
			Name:    e.Name,
			Cmd:     e.Cmd,
			Args:    e.Args,
			Timeout: timeout,
		}, p.cfg.Src, p.cfg.Lang)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			p.logger.WarnContext(ctx, "extractor failed", "name", e.Name, "err", err)
			status[e.Name] = "failed"
			p.sink.Emit(ui.ExtractorFinished{Name: e.Name, Status: "failed"})
			continue
		}
		status[e.Name] = "ok"
		facts = append(facts, extracted...)
		p.sink.Emit(ui.ExtractorFinished{Name: e.Name, Status: "ok", Facts: len(extracted)})
	}
	return facts, status, nil
}

// loadSpecs reads the --inherit specs: their nodes, by shared root name.
func loadSpecs(dirs map[string]string) (map[string][]graph.Node, error) {
	specs := make(map[string][]graph.Node, len(dirs))
	for name, dir := range dirs {
		nodes, err := render.LoadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("--inherit %s: %w", name, err)
		}
		if len(nodes) == 0 {
			return nil, fmt.Errorf("--inherit %s: %s holds no spec nodes", name, dir)
		}
		specs[name] = nodes
	}
	return specs, nil
}

// inheritedRules are the rules of each inherited spec that the reach facts
// among facts cover.
func inheritedRules(specs map[string][]graph.Node, facts []extract.Fact) []extract.Fact {
	var rules []extract.Fact
	for _, name := range slices.Sorted(maps.Keys(specs)) {
		rules = append(rules, inherit.Rules(specs[name], name, facts)...)
	}
	return rules
}

// loadFactsFiles reads the --facts files (layer C).
func loadFactsFiles(paths []string) ([]extract.Fact, error) {
	var facts []extract.Fact
	for _, path := range paths {
		loaded, err := extract.LoadFactsFile(path)
		if err != nil {
			return nil, err
		}
		facts = append(facts, loaded...)
	}
	return facts, nil
}

// chunkFiles cuts every read, non-generated file into chunks, sorted by
// path then line, and returns the generated files left out as coverage
// gaps of the domain their code would have had: their rules are their
// generator's, so they keep their facts but are not mapped.
func chunkFiles(
	ext *extraction,
	domainOf func(namespace, path string, line int) string,
) ([]sitter.Chunk, []graph.CoverageGap) {
	var chunks []sitter.Chunk
	var generated []graph.CoverageGap
	for _, f := range ext.files {
		data, ok := ext.fileData[f.Path]
		if !ok {
			continue
		}
		if f.Generated {
			namespace := sitter.FileNamespace(ext.fileInfo[f.Path])
			generated = append(generated, graph.CoverageGap{
				Domain: domainOf(namespace, f.Path, 1),
				Reason: graph.GapGenerated,
				Path:   f.Path,
			})
			continue
		}
		chunks = append(chunks,
			sitter.ChunkFile(f.Path, f.Language, data, ext.fileInfo[f.Path], domainOf)...)
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].Path != chunks[j].Path {
			return chunks[i].Path < chunks[j].Path
		}
		return chunks[i].StartLine < chunks[j].StartLine
	})
	return chunks, generated
}

// mapChunks runs the map phase: candidate rules per chunk.
func (p *pipeline) mapChunks(
	ctx context.Context,
	chunks []sitter.Chunk,
	idx *factIndex,
	groundingOpts grounding.Options,
) ([]mapper.Output, error) {
	if err := p.enter("map", func(s *state.State) {
		s.ChunksTotal, s.ChunksDone, s.ChunksFailed = len(chunks), 0, 0
	}); err != nil {
		return nil, err
	}
	m := &mapper.Mapper{
		Log: p.logger,
		Ungrounded: func(requirement string, cited []string) []string {
			result := grounding.Check(requirement,
				[]grounding.Citation{{Start: 1, End: len(cited), File: cited}}, groundingOpts)
			values := make([]string, len(result.Missing))
			for i, miss := range result.Missing {
				values[i] = miss.Literal
			}
			return values
		},
		LLM:          p.client,
		Model:        p.cfg.Model,
		Lang:         p.cfg.Lang,
		Workers:      p.cfg.Workers,
		Reground:     p.cfg.Reground,
		OutDir:       filepath.Join(p.cfg.Out, ".codetospec", "map"),
		Entities:     idx.entities,
		EndpointsFor: func(path string) []string { return idx.endpointsByPath[path] },
		SymbolsFor:   func(path string) []mapper.SymbolContext { return idx.symbols[path] },
		RulesFor:     func(path string) []mapper.Note { return idx.mechanical[path] },
		DocsFor:      func(path string) []mapper.Note { return idx.docs[path] },
		OnUnit: func(out mapper.Output, usage llm.Usage) {
			var done int
			p.record(ctx, func(s *state.State) {
				s.ChunksDone++
				if out.Failed {
					s.ChunksFailed++
				}
				s.AddTokens("map", usage.PromptTokens, usage.CompletionTokens)
				done = s.ChunksDone
			})
			p.sink.Emit(ui.MapUnit{
				Path:   out.Path,
				Lines:  fmt.Sprintf("%d-%d", out.StartLine, out.EndLine),
				Domain: out.Domain,
				Rules:  len(out.Rules),
				Failed: out.Failed,
				Done:   done,
				Total:  len(chunks),
				Usage:  usage,
			})
		},
	}
	return m.Run(ctx, chunks)
}

// record updates the state store from a progress callback, where a failed
// save can only be reported: the run goes on and the next save retries.
func (p *pipeline) record(ctx context.Context, update func(*state.State)) {
	if err := p.st.Update(update); err != nil {
		p.logger.WarnContext(ctx, "state save failed", "err", err)
	}
}

// coverageGaps lists, per domain, the code that produced no rule: the
// generated files chunkFiles left out, the chunks whose map failed and the
// domains whose reduce failed.
func coverageGaps(
	generated []graph.CoverageGap,
	mapped []mapper.Output,
	reduced []reducer.Output,
) []graph.CoverageGap {
	gaps := slices.Clone(generated)
	for _, out := range mapped {
		if out.Failed {
			gaps = append(gaps, graph.CoverageGap{
				Domain: out.Domain,
				Reason: graph.GapMapFailed,
				Path:   out.Path,
				Lines:  fmt.Sprintf("%d-%d", out.StartLine, out.EndLine),
			})
		}
	}
	for _, out := range reduced {
		if out.Failed {
			gaps = append(
				gaps,
				graph.CoverageGap{Domain: out.Domain, Reason: graph.GapReduceFailed},
			)
		}
	}
	return gaps
}

// countGaps counts the gaps with reason.
func countGaps(gaps []graph.CoverageGap, reason graph.GapReason) int {
	n := 0
	for _, g := range gaps {
		if g.Reason == reason {
			n++
		}
	}
	return n
}

// candidatesByDomain groups the rules of successful map outputs by domain.
func candidatesByDomain(outputs []mapper.Output) map[string][]mapper.Rule {
	candidates := make(map[string][]mapper.Rule)
	for _, out := range outputs {
		if !out.Failed && len(out.Rules) > 0 {
			candidates[out.Domain] = append(candidates[out.Domain], out.Rules...)
		}
	}
	return candidates
}

// reduce runs the reduce phase: one consolidated rule set per domain.
func (p *pipeline) reduce(
	ctx context.Context,
	candidates map[string][]mapper.Rule,
	idx *factIndex,
) ([]reducer.Output, error) {
	if err := p.enter("reduce", func(s *state.State) {
		s.DomainsTotal, s.DomainsDone, s.DomainsFailed = len(candidates), 0, 0
	}); err != nil {
		return nil, err
	}
	r := &reducer.Reducer{
		Log:       p.logger,
		LLM:       p.client,
		Model:     p.cfg.Model,
		Lang:      p.cfg.Lang,
		OutDir:    filepath.Join(p.cfg.Out, ".codetospec", "reduce"),
		Entities:  idx.entities,
		Endpoints: idx.allEndpoints,
		BatchSize: p.cfg.ReduceBatch,
		Mode:      p.cfg.ReduceMode,
		Workers:   p.cfg.Workers,
		OnUnit: func(out reducer.Output, usage llm.Usage) {
			var done int
			p.record(ctx, func(s *state.State) {
				s.DomainsDone++
				if out.Failed {
					s.DomainsFailed++
				}
				s.AddTokens("reduce", usage.PromptTokens, usage.CompletionTokens)
				done = s.DomainsDone
			})
			p.sink.Emit(ui.ReduceUnit{
				Domain: out.Domain,
				Rules:  len(out.Rules),
				Failed: out.Failed,
				Done:   done,
				Total:  len(candidates),
				Usage:  usage,
			})
		},
	}
	return r.Run(ctx, candidates)
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
	nodes = grounding.Annotate(roots, nodes, opts)
	for i := range nodes {
		if nodes[i].Type != graph.TypeRule {
			continue
		}
		digest, err := drift.Digest(roots, nodes[i].Sources)
		if err != nil {
			continue // a broken citation is caught by verify
		}
		if nodes[i].Extra == nil {
			nodes[i].Extra = map[string]string{}
		}
		nodes[i].Extra["digest"] = digest
		callees := drift.Callees(nodes[i].Sources, facts)
		if calleesDigest, calleeErr := drift.Digest(
			roots,
			callees,
		); calleeErr == nil &&
			len(callees) > 0 {
			nodes[i].Callees = callees
			nodes[i].Extra[graph.ExtraCalleesDigest] = calleesDigest
		}
	}
	return nodes
}

// errVerification means the built graph failed its checks: nothing is
// rendered.
var errVerification = errors.New("graph verification failed, nothing rendered")

// verify checks the built graph; each violation is logged and kept for the
// run summary.
func (p *pipeline) verify(ctx context.Context, nodes []graph.Node) error {
	violations := verify.Run(nodes, p.cfg.Roots())
	if len(violations) == 0 {
		return nil
	}
	for _, v := range violations {
		p.final.Violations = append(p.final.Violations, v.String())
		p.logger.ErrorContext(ctx, "verification failed",
			"node", v.NodeID, "check", v.Check, "detail", v.Detail)
	}
	return fmt.Errorf("%w: %d violation(s)", errVerification, len(violations))
}

// metaInputs is what the README summary needs beyond the configuration
// and the extraction.
type metaInputs struct {
	chunks   []sitter.Chunk
	gaps     []graph.CoverageGap // generated files and failed map or reduce units
	groups   map[string]string
	nodes    []graph.Node
	tally    crosscheck.Tally
	snapshot *state.State
	models   *domainmodel.Set // nil without --domain-model
}

// buildMeta computes the output README's summary and checks.
func buildMeta(cfg *config.Config, ext *extraction, in *metaInputs) render.Meta {
	meta := render.Meta{
		DomainSplits:          communities.Splits(codeFiles(in.chunks), importsOf(ext.facts)),
		DomainGroups:          in.groups,
		Coverage:              graph.ComputeCoverage(in.nodes),
		ChunksFailed:          in.snapshot.ChunksFailed,
		ChunksTotal:           in.snapshot.ChunksTotal,
		DomainsFailed:         in.snapshot.DomainsFailed,
		DomainsTotal:          in.snapshot.DomainsTotal,
		FilesNoGrammar:        ext.noGrammar,
		FilesGenerated:        countGaps(in.gaps, graph.GapGenerated),
		CoverageGaps:          graph.SortGaps(in.gaps),
		Crosschecked:          cfg.Crosscheck,
		CrosscheckSupported:   in.tally.Supported,
		CrosscheckPartial:     in.tally.Partial,
		CrosscheckUnsupported: in.tally.Unsupported,
		CrosscheckRepaired:    in.tally.Repaired,
		CrosscheckUnchecked:   in.tally.Unchecked,
		CrosscheckMechanical:  in.tally.Mechanical,
		DuplicatePairs:        consistency.FindDuplicates(in.nodes, consistency.DefaultThreshold),
		SharedEntities:        graph.SharedEntities(in.nodes),
		Lang:                  cfg.Lang,
		RuleFactsRejected:     len(ext.rejected),
	}
	for _, name := range sortedKeys(ext.status) {
		if ext.status[name] != "ok" {
			meta.ExtractorsFailed = append(meta.ExtractorsFailed, name)
		}
	}
	if in.models != nil {
		report := domainmodel.Compare(in.models, in.nodes,
			domainmodel.Options{Entities: cfg.DomainModelEntities})
		meta.DomainModel = &report
	}
	return meta
}

// summarize completes the run summary with the run's counts and token use.
func (p *pipeline) summarize(nodes []graph.Node, coverage graph.Coverage, snapshot *state.State) {
	final := &p.final
	final.NodesByType = map[string]int{}
	for i := range nodes {
		final.NodesByType[nodes[i].Type]++
	}
	final.Coverage = coverage
	final.ChunksFailed = snapshot.ChunksFailed
	final.ChunksTotal = snapshot.ChunksTotal
	final.DomainsFailed = snapshot.DomainsFailed
	final.DomainsTotal = snapshot.DomainsTotal
	for _, phase := range state.Phases() {
		if t := snapshot.Tokens[phase]; t != nil {
			final.Tokens = append(final.Tokens, ui.PhaseUsage{
				Phase: phase,
				Usage: llm.Usage{PromptTokens: t.LastPrompt, CompletionTokens: t.LastCompletion},
			})
		}
	}
}

// writeFactsFile persists the merged facts under <out>/.codetospec/facts.json.
func writeFactsFile(outDir string, facts []extract.Fact) error {
	data, err := json.MarshalIndent(
		extract.FactsEnvelope{Schema: extract.FactsSchema, Facts: facts},
		"",
		"  ",
	)
	if err != nil {
		return fmt.Errorf("encode facts.json: %w", err)
	}
	return atomicfs.WriteFile(
		filepath.Join(outDir, ".codetospec", "facts.json"),
		append(data, '\n'),
	)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// codeFiles lists each chunked file with its namespace and the domains of
// its chunks, sorted.
func codeFiles(chunks []sitter.Chunk) []communities.File {
	byPath := make(map[string]*communities.File)
	var files []*communities.File
	for _, c := range chunks {
		f := byPath[c.Path]
		if f == nil {
			f = &communities.File{Path: c.Path, Namespace: c.Namespace}
			byPath[c.Path] = f
			files = append(files, f)
		}
		if !slices.Contains(f.Domains, c.Domain) {
			f.Domains = append(f.Domains, c.Domain)
		}
	}
	out := make([]communities.File, len(files))
	for i, f := range files {
		out[i] = *f
	}
	return out
}

// importsOf lists the "import" facts as communities imports.
func importsOf(facts []extract.Fact) []communities.Import {
	var imports []communities.Import
	for _, f := range facts {
		if f.Kind == "import" {
			imports = append(
				imports,
				communities.Import{From: f.Source.Path, Target: f.Attrs["target"]},
			)
		}
	}
	return imports
}

// domainGroups maps each domain that chunks of an ambiguous namespace
// resolved to by directory (no extractor hint) to that namespace; a domain
// reached from two such namespaces belongs to neither.
func domainGroups(
	chunks []sitter.Chunk,
	ambiguous map[string]bool,
	hints *extract.DomainHints,
) map[string]string {
	groups := make(map[string]string)
	mixed := make(map[string]bool)
	for _, c := range chunks {
		if !ambiguous[c.Namespace] || hints.At(c.Path, c.StartLine) != "" {
			continue
		}
		if ns, ok := groups[c.Domain]; ok && ns != c.Namespace {
			mixed[c.Domain] = true
		}
		groups[c.Domain] = c.Namespace
	}
	for domain := range mixed {
		delete(groups, domain)
	}
	return groups
}

// domainModel builds the modelith models to write when
// --write-domain-model is set; without it, nothing (no files).
func (p *pipeline) domainModel(
	models *domainmodel.Set,
	nodes []graph.Node,
	tables []domainmodel.Table,
) (domainmodel.Enhanced, error) {
	if !p.cfg.WriteDomainModel {
		return domainmodel.Enhanced{}, nil
	}
	written, err := domainmodel.Enhance(&domainmodel.EnhanceInput{
		Set:        models,
		Nodes:      nodes,
		Tables:     tables,
		Lang:       p.cfg.Lang,
		Options:    domainmodel.Options{Entities: p.cfg.DomainModelEntities},
		MirrorPath: mirrorPath(p.cfg.Src),
	})
	if err != nil {
		return domainmodel.Enhanced{}, err
	}
	return *written, nil
}

// tablesOf turns the table facts into the code entities a domain model is
// written from; domainOf places each by its declaration.
func tablesOf(
	facts []extract.Fact,
	domainOf func(namespace, path string, line int) string,
) []domainmodel.Table {
	var tables []domainmodel.Table
	for _, f := range facts {
		if f.Kind != "table" {
			continue
		}
		title := cmp.Or(f.Attrs["name"], strings.TrimPrefix(f.ID, "table."))
		var columns []string
		for column := range strings.SplitSeq(f.Attrs["columns"], ",") {
			if column = strings.TrimSpace(column); column != "" {
				columns = append(columns, column)
			}
		}
		line := 0
		if start, _, err := extract.ParseLines(f.Source.Lines); err == nil {
			line = start
		}
		tables = append(tables, domainmodel.Table{
			ID:          graph.EntityID(&f),
			Title:       title,
			Columns:     columns,
			Description: f.Attrs[extract.AttrDescription],
			Source:      f.Source,
			Domain:      domainOf(f.Attrs["namespace"], f.Source.Path, line),
		})
	}
	return tables
}

// mirrorPath places a human model's enhanced copy at its path relative to
// src, or under its base name when it lies outside src.
func mirrorPath(src string) func(string) string {
	return func(modelPath string) string {
		absSrc, srcErr := filepath.Abs(src)
		absModel, modelErr := filepath.Abs(modelPath)
		if srcErr == nil && modelErr == nil {
			if rel, err := filepath.Rel(
				absSrc,
				absModel,
			); err == nil &&
				!strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.Base(modelPath)
	}
}

// writeModels replaces <out>/models with the written model files and their
// provenance; without --write-domain-model it leaves the directory alone.
func (p *pipeline) writeModels(written *domainmodel.Enhanced) error {
	if !p.cfg.WriteDomainModel {
		return nil
	}
	dir := filepath.Join(p.cfg.Out, "models")
	if len(written.Files) == 0 {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("clear models dir: %w", err)
		}
		return nil
	}
	return atomicfs.ReplaceDir(dir, func(staging string) error {
		return writeModelFiles(staging, written)
	})
}

// writeModelFiles writes the models and their provenance.json under dir.
func writeModelFiles(dir string, written *domainmodel.Enhanced) error {
	for _, f := range written.Files {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("create models dir: %w", err)
		}
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			return fmt.Errorf("write model %s: %w", f.Path, err)
		}
	}
	provenance, err := json.MarshalIndent(written.Provenance, "", "  ")
	if err != nil {
		return fmt.Errorf("encode model provenance: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "provenance.json"), append(provenance, '\n'), 0o600)
}
