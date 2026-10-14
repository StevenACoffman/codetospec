package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"slices"
	"sort"
	"strconv"
	"strings"

	gqlast "github.com/vektah/gqlparser/v2/ast"
	gqlparser "github.com/vektah/gqlparser/v2/parser"
)

const (
	// composedSchemaFile is the federated supergraph schema, relative to
	// Inputs.Supergraph.
	composedSchemaFile = "composed_schema.graphql"
	// genqlientQueries holds the service's genqlient operations, relative
	// to the service root.
	genqlientQueries = "generated/genqlient/queries.go"
	// operationSuffix ends the constant holding an operation's text.
	operationSuffix = "_Operation"
)

// dependencyFacts records every call to a genqlient operation whose root
// fields another service owns: one fact per call site and owning service,
// naming the field ("Mutation.acceptInvitation").
func dependencyFacts(in *Inputs, sources []source) ([]Fact, []string) {
	if in.Supergraph == nil {
		return nil, []string{"dependencies: no supergraph schema, cross-service calls not resolved"}
	}
	owners, err := fieldOwners(in.Supergraph)
	if err != nil {
		return nil, []string{fmt.Sprintf("dependencies: %v", err)}
	}
	ops, warnings, err := genqlientOperations(in.Service)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // the service calls no other service through genqlient
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("dependencies: %v", err)}
	}

	var facts []Fact
	for i := range sources {
		for _, call := range genqlientCalls(&sources[i]) {
			facts = append(facts, call.facts(sources[i].name, ops, owners, in.ServiceGraph)...)
		}
	}
	return facts, warnings
}

// genqlientCall is one call of a genqlient operation function.
type genqlientCall struct {
	name string // operation function, e.g. "Users_AcceptInvitation"
	line int
	as   string // the client's authority (see clientAuthority); "" if unknown
}

// genqlientCalls finds the genqlient.<Operation>(…) calls in s.
func genqlientCalls(s *source) []genqlientCall {
	var calls []genqlientCall
	ast.Inspect(s.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "genqlient" {
			calls = append(calls, genqlientCall{
				name: sel.Sel.Name,
				line: s.fset.Position(call.Pos()).Line,
				as:   clientAuthority(call),
			})
		}
		return true
	})
	return calls
}

// facts returns the dependency facts of a call in file: one per root field
// of its operation (ops) and owning service (owners) but self.
func (c *genqlientCall) facts(file string, ops, owners map[string][]string, self string) []Fact {
	var facts []Fact
	for _, field := range ops[c.name] {
		for _, owner := range owners[field] {
			if owner == self {
				continue
			}
			f := dependencyFact(file, c.line, serviceSlug(owner), field)
			if c.as != "" {
				f.Attrs[attrAs] = c.as
			}
			facts = append(facts, f)
		}
	}
	return facts
}

// clientArg is the position of the client in a genqlient call:
// genqlient.Op(ctx, client, …).
const clientArg = 1

// clientAuthority is the authority of a genqlient call's client, when the
// call builds it in place: ctx.GraphQL().AsServiceAdmin() → asServiceAdmin,
// AsUser() → asUser; "" for a client held in a variable.
func clientAuthority(call *ast.CallExpr) string {
	if len(call.Args) <= clientArg {
		return ""
	}
	client, ok := call.Args[clientArg].(*ast.CallExpr)
	if !ok {
		return ""
	}
	switch calledName(client) {
	case "AsServiceAdmin":
		return asServiceAdmin
	case "AsUser":
		return asUser
	}
	return ""
}

func dependencyFact(file string, line int, service, field string) Fact {
	return Fact{
		Kind:      kindDependency,
		ID:        fmt.Sprintf("dependency.webapp.%s:%d:%s:%s", file, line, service, field),
		Attrs:     map[string]string{attrService: service, attrOperation: field},
		Source:    Ref{Path: file, Lines: lines(line, line)},
		Origin:    origin,
		Certainty: certain,
	}
}

// serviceSlug names a subgraph as a service slug: AI_GUIDE → ai-guide.
func serviceSlug(graph string) string {
	return slugify(strings.ReplaceAll(graph, "_", "-"))
}

// fieldOwners maps each root field ("Mutation.acceptInvitation") of the
// supergraph to the subgraphs that resolve it: its @join__field graphs, or
// else its type's single @join__type graph.
func fieldOwners(supergraph fs.FS) (map[string][]string, error) {
	src, err := fs.ReadFile(supergraph, composedSchemaFile)
	if err != nil {
		return nil, fmt.Errorf("read supergraph: %w", err)
	}
	doc, err := gqlparser.ParseSchema(&gqlast.Source{Name: composedSchemaFile, Input: string(src)})
	if err != nil {
		return nil, fmt.Errorf("parse supergraph: %w", err)
	}
	owners := make(map[string][]string)
	for _, defs := range []gqlast.DefinitionList{doc.Definitions, doc.Extensions} {
		for _, def := range defs {
			if !isRootType(def.Name) {
				continue
			}
			typeGraphs := joinGraphs(def.Directives, "join__type")
			for _, f := range def.Fields {
				graphs := joinGraphs(f.Directives, "join__field")
				if len(graphs) == 0 && len(typeGraphs) == 1 {
					graphs = typeGraphs
				}
				key := def.Name + "." + f.Name
				owners[key] = append(owners[key], graphs...)
			}
		}
	}
	for key, graphs := range owners {
		sort.Strings(graphs)
		owners[key] = slices.Compact(graphs)
	}
	return owners, nil
}

// joinGraphs returns the graph argument of every directive named name.
func joinGraphs(directives gqlast.DirectiveList, name string) []string {
	var graphs []string
	for _, d := range directives.ForNames(name) {
		if arg := d.Arguments.ForName("graph"); arg != nil && arg.Value != nil {
			graphs = append(graphs, arg.Value.Raw)
		}
	}
	return graphs
}

// genqlientOperations maps each genqlient operation function name to the
// root fields its query selects ("Query.classroom").
func genqlientOperations(service fs.FS) (map[string][]string, []string, error) {
	s, err := parseGo(service, genqlientQueries)
	if err != nil {
		return nil, nil, err
	}
	ops := make(map[string][]string)
	var warnings []string
	for _, decl := range s.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			name, query, isOp := operationText(spec)
			if !isOp {
				continue
			}
			fields, parseErr := rootFields(query)
			if parseErr != nil {
				warnings = append(
					warnings,
					fmt.Sprintf("dependencies: operation %s: %v", name, parseErr),
				)
				continue
			}
			ops[name] = fields
		}
	}
	return ops, warnings, nil
}

// operationText recognizes `const <Name>_Operation = "<query>"`, returning
// the operation function name and its query text.
func operationText(spec ast.Spec) (string, string, bool) {
	vs, isValue := spec.(*ast.ValueSpec)
	if !isValue || len(vs.Names) != 1 || len(vs.Values) != 1 {
		return "", "", false
	}
	name, found := strings.CutSuffix(vs.Names[0].Name, operationSuffix)
	lit, isLit := vs.Values[0].(*ast.BasicLit)
	if !found || !isLit || lit.Kind != token.STRING {
		return "", "", false
	}
	query, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", "", false
	}
	return name, query, true
}

// rootFields returns the root fields an operation selects, qualified by
// their root type.
func rootFields(query string) ([]string, error) {
	doc, err := gqlparser.ParseQuery(&gqlast.Source{Input: query})
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var fields []string
	for _, op := range doc.Operations {
		root := gqlQuery
		switch op.Operation {
		case gqlast.Mutation:
			root = "Mutation"
		case gqlast.Subscription:
			root = "Subscription"
		case gqlast.Query:
		}
		for _, sel := range op.SelectionSet {
			if f, ok := sel.(*gqlast.Field); ok && !strings.HasPrefix(f.Name, "__") {
				fields = append(fields, root+"."+f.Name)
			}
		}
	}
	return fields, nil
}
