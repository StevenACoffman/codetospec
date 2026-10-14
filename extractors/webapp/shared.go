package main

import (
	"fmt"
	"go/ast"
	"path"
)

// sharedDomainPrefix starts the domain of a rule webapp's shared code
// gives a service, so it never merges with one of the service's own.
const sharedDomainPrefix = "shared-"

// sharedFacts returns the rules webapp's shared code (pkg/) enforces on
// the service, derived as the service's own are: the guards of a checking
// wrapper (serve.ValidateGraphqlSecretMiddleware, serve.CreateTaskHandler)
// and the policies, of the functions the service reaches (see reached).
// They are cited in pkg/, under the webappRoot citation root.
func sharedFacts(shared sharedCode, reach map[declKey]bool, known checks, w *wording) []Fact {
	idx := indexFuncs(shared.sources)
	var facts []Fact
	for i := range shared.sources {
		s := &shared.sources[i]
		dir, pkg := path.Dir(s.name), s.file.Name.Name
		sc := scanner{fset: s.fset, known: known}
		for _, fn := range funcDecls(s.file) {
			if fn.Recv != nil || !reach[declKey{dir, fn.Name.Name}] {
				continue
			}
			// Named as declared, so the cited code grounds it; the domain
			// (see rooted) says which package.
			op := operation{name: fn.Name.Name}
			var m resolverMethod
			if lit, ok := returnedClosure(fn.Body); ok {
				// Cited from the declaration, which names the wrapper.
				declared := s.fset.Position(fn.Pos()).Line
				for _, g := range sc.guards(lit.Body) {
					g.start = min(g.start, declared)
					m.guards = append(m.guards, g)
				}
				op.name = fmt.Sprintf(w.throughFmt, op.name)
			}
			if g, isPolicy := sc.permit(fn, idx.denier(pkg)); isPolicy {
				m.guards = append(m.guards, g)
			}
			facts = append(facts, rooted(ruleFacts(s.fset, w, s.name, &m, &op))...)
		}
	}
	return facts
}

// rooted marks facts as cited in webapp's shared code, in the shared
// domain of their package (shared-web for pkg/web), never one of the
// service's own: the one place a shared fact's citation and domain are set.
func rooted(facts []Fact) []Fact {
	for i := range facts {
		facts[i].Source.Root = webappRoot
		if _, ok := facts[i].Attrs[attrDomain]; ok {
			facts[i].Attrs[attrDomain] = sharedDomainPrefix + domainOf(
				path.Dir(facts[i].Source.Path),
			)
		}
	}
	return facts
}

// addSharedTexts sets the phrases of shared rules.
func addSharedTexts(w *wording, tr translate) {
	w.throughFmt = tr("les requêtes passant par `%s`", "requests through `%s`")
}

// returnedClosure returns the closure a function body ends by returning:
// return func(w, r) {…}, or return http.HandlerFunc(func(w, r) {…}).
func returnedClosure(body *ast.BlockStmt) (*ast.FuncLit, bool) {
	if len(body.List) == 0 {
		return nil, false
	}
	ret, ok := body.List[len(body.List)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil, false
	}
	result := ret.Results[0]
	if conv, isCall := result.(*ast.CallExpr); isCall && len(conv.Args) == 1 {
		result = conv.Args[0]
	}
	lit, isLit := result.(*ast.FuncLit)
	return lit, isLit
}
