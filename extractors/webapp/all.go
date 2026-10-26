package main

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// servicesDir holds webapp's services, one directory each.
const servicesDir = "services"

// attrInServices names, on a consolidated fact, the services it comes from,
// comma-separated: one for a service's own fact, several for a rule of
// shared code they all reach.
const attrInServices = "in_services"

// AllInputs are what AnalyzeAll reads: a whole webapp checkout.
type AllInputs struct {
	Webapp     fs.FS // the checkout root: services/, pkg/, gengraphql/, go.mod
	Lang       string
	AccessPkgs []string
}

// AnalyzeAll analyzes every webapp service holding Go code, as Analyze does
// one, and consolidates their facts relative to the checkout root (see
// consolidate): one listing of webapp's authorization rules, each shared
// rule once. A service that cannot be read is a warning, not an error.
//
// Ensures: facts are sorted (sortFacts); warnings are prefixed by service.
func AnalyzeAll(in *AllInputs) ([]Fact, []string, error) {
	services, err := goServices(in.Webapp)
	if err != nil {
		return nil, nil, err
	}
	var supergraph fs.FS
	if sub, subErr := fs.Sub(in.Webapp, "gengraphql"); subErr == nil {
		if _, statErr := fs.Stat(sub, composedSchemaFile); statErr == nil {
			supergraph = sub
		}
	}
	merged := make(map[string]*Fact)
	var warnings []string
	for _, svc := range services {
		facts, svcWarnings, svcErr := analyzeService(in, supergraph, svc)
		for _, w := range svcWarnings {
			warnings = append(warnings, svc+": "+w)
		}
		if svcErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: skipped: %v", svc, svcErr))
			continue
		}
		consolidated := consolidate(svc, facts)
		for i := range consolidated {
			mergeFact(merged, &consolidated[i])
		}
	}
	out := make([]Fact, 0, len(merged))
	for _, f := range merged {
		out = append(out, *f)
	}
	sortFacts(out)
	return out, warnings, nil
}

// analyzeService analyzes services/<svc> of the checkout.
func analyzeService(in *AllInputs, supergraph fs.FS, svc string) ([]Fact, []string, error) {
	sub, err := fs.Sub(in.Webapp, path.Join(servicesDir, svc))
	if err != nil {
		return nil, nil, fmt.Errorf("open: %w", err)
	}
	return Analyze(&Inputs{
		Service:      sub,
		Webapp:       in.Webapp,
		Supergraph:   supergraph,
		ServiceGraph: strings.ToUpper(strings.ReplaceAll(svc, "-", "_")),
		Lang:         in.Lang,
		AccessPkgs:   in.AccessPkgs,
	})
}

// goServices lists the services of the checkout holding non-test Go code,
// sorted; the others (JavaScript, VCL) are no business of this extractor.
func goServices(webapp fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(webapp, servicesDir)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	var services []string
	for _, e := range entries {
		dir := path.Join(servicesDir, e.Name())
		if info, statErr := fs.Stat(webapp, dir); statErr != nil || !info.IsDir() {
			continue
		}
		sub, subErr := fs.Sub(webapp, dir)
		if subErr != nil {
			continue
		}
		if names, listErr := goSources(sub); listErr == nil && len(names) > 0 {
			services = append(services, e.Name())
		}
	}
	return services, nil
}

// consolidate rebases one service's facts onto the checkout root: a
// citation of the service moves under services/<svc>/, one of shared code
// (pkgRoot) moves under pkg/ and loses its root; ids,
// domains and route links of the service's own facts are namespaced by the
// service, so services never collide while a shared fact keeps its id and
// merges (see mergeFact). Every fact records its service (attrInServices).
func consolidate(svc string, facts []Fact) []Fact {
	out := make([]Fact, len(facts))
	for i, f := range facts {
		attrs := make(map[string]string, len(f.Attrs)+1)
		for k, v := range f.Attrs {
			attrs[k] = v
		}
		attrs[attrInServices] = svc
		switch {
		case f.Source.Root == pkgRoot:
			f.Source.Path, f.Source.Root = path.Join(sharedDir, f.Source.Path), ""
		case f.Source.Path != "":
			f.Source.Path = path.Join(servicesDir, svc, f.Source.Path)
			f.ID = svc + ":" + f.ID
			namespaceAttrs(svc, attrs)
		}
		f.Attrs = attrs
		out[i] = f
	}
	return out
}

// namespaceAttrs rebases the attributes of a service's own fact that name
// something of the service: its route link, handler file and domain.
func namespaceAttrs(svc string, attrs map[string]string) {
	if route := attrs[attrRoute]; route != "" {
		attrs[attrRoute] = svc + ":" + route
	}
	if file := attrs[attrHandlerPath]; file != "" {
		attrs[attrHandlerPath] = path.Join(servicesDir, svc, file)
	}
	if domain := attrs[attrDomain]; domain != "" {
		attrs[attrDomain] = slugify(svc + "-" + domain)
	}
}

// mergeFact adds f to merged, or, for a fact merged already (a shared
// rule, an http.Status constant), adds its services to the merged one's.
func mergeFact(merged map[string]*Fact, f *Fact) {
	have, ok := merged[f.ID]
	if !ok {
		merged[f.ID] = f
		return
	}
	services := strings.Split(have.Attrs[attrInServices], ",")
	for _, svc := range strings.Split(f.Attrs[attrInServices], ",") {
		if !slices.Contains(services, svc) {
			services = append(services, svc)
		}
	}
	slices.Sort(services)
	have.Attrs[attrInServices] = strings.Join(services, ",")
}
