package main

import (
	"go/ast"
	"sort"
)

// helperFn is one package-level function of the resolver package.
type helperFn struct {
	file       *source
	start, end int
}

// helperDomainFacts gives each package-level function of the resolver files
// the domain of its callers when they all agree: a helper serves the
// operations that call it, even from another file (acls.go, or classroom.go
// whose methods span several domains). Callers are resolver methods, whose
// domain the method facts give, and other helpers, resolved in turn until
// nothing changes. A helper with no caller, or with callers in two domains,
// gets no fact: codetospec's own strategy decides.
func helperDomainFacts(resolverFiles []*source, facts []Fact) []Fact {
	methodDomain := methodDomains(facts)
	helpers := make(map[string]*helperFn)
	callers := make(map[string][]string) // helper → caller keys
	for _, s := range resolverFiles {
		for _, decl := range s.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			start, end := s.fset.Position(fn.Pos()).Line, s.fset.Position(fn.End()).Line
			key := s.name + ":" + lines(start, end)
			if fn.Recv == nil {
				helpers[fn.Name.Name] = &helperFn{file: s, start: start, end: end}
				key = fn.Name.Name
			}
			for _, callee := range calledNames(fn.Body) {
				callers[callee] = append(callers[callee], key)
			}
		}
	}
	domain := resolveHelperDomains(helpers, callers, methodDomain)
	names := make([]string, 0, len(domain))
	for name := range domain {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Fact, 0, len(names))
	for _, name := range names {
		h := helpers[name]
		out = append(out, domainFact(h.file.name, h.start, h.end, domain[name]))
	}
	return out
}

// methodDomains indexes the domain facts of resolver methods by
// "file:start-end", the caller key of a method.
func methodDomains(facts []Fact) map[string]string {
	domains := make(map[string]string)
	for _, f := range facts {
		if f.Kind == kindDomain {
			domains[f.Source.Path+":"+f.Source.Lines] = f.Attrs[attrDomain]
		}
	}
	return domains
}

// resolveHelperDomains computes, to a fixed point, the domain each helper's
// callers agree on: a caller key is a method ("file:lines", in
// methodDomain) or another helper's name.
func resolveHelperDomains(
	helpers map[string]*helperFn,
	callers map[string][]string,
	methodDomain map[string]string,
) map[string]string {
	names := make([]string, 0, len(helpers))
	for name := range helpers {
		names = append(names, name)
	}
	sort.Strings(names)
	domain := make(map[string]string)
	for changed := true; changed; {
		changed = false
		for _, name := range names {
			if _, done := domain[name]; done {
				continue
			}
			if d, ok := agreedDomain(callers[name], methodDomain, domain); ok {
				domain[name] = d
				changed = true
			}
		}
	}
	return domain
}

// agreedDomain returns the one domain of the callers that have one, false
// when they disagree or none has one yet.
func agreedDomain(
	callerKeys []string,
	methodDomain, helperDomain map[string]string,
) (string, bool) {
	agreed := ""
	for _, key := range callerKeys {
		d := methodDomain[key]
		if d == "" {
			d = helperDomain[key]
		}
		switch {
		case d == "":
			continue
		case agreed == "":
			agreed = d
		case d != agreed:
			return "", false
		}
	}
	return agreed, agreed != ""
}

// calledNames lists the plain-identifier calls in body: calls of package
// functions, helpers included.
func calledNames(body *ast.BlockStmt) []string {
	var names []string
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ident, isIdent := call.Fun.(*ast.Ident); isIdent {
				names = append(names, ident.Name)
			}
		}
		return true
	})
	return names
}
