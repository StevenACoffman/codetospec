package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// sqlNameRe finds the names after the SQL keywords that name a table a
// statement reads or writes: FROM (DELETE FROM too), JOIN, INTO, UPDATE.
// A name may be quoted and schema-qualified.
var sqlNameRe = regexp.MustCompile(`(?i)\b(?:from|join|into|update)\s+` +
	`("?[a-z_][a-z0-9_$]*"?(?:\s*\.\s*"?[a-z_][a-z0-9_$]*"?)?)`)

// sqlNames lists the names text's SQL puts after FROM, JOIN, INTO or
// UPDATE, lowercased and unquoted, in order of first appearance. Not all
// are tables (a CTE, a function's FROM): callers keep the names a schema
// declares.
func sqlNames(text string) []string {
	var names []string
	for _, m := range sqlNameRe.FindAllStringSubmatch(text, -1) {
		name := strings.ToLower(
			strings.NewReplacer(`"`, "", " ", "", "\t", "", "\n", "").Replace(m[1]),
		)
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// queryNames lists the SQL names (see sqlNames) of the string literals in
// body and of the package-level string constants it uses: where sqlc's
// generated methods keep their queries.
func queryNames(body ast.Node, info *types.Info) []string {
	var names []string
	add := func(text string) {
		for _, n := range sqlNames(text) {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if text, err := strconv.Unquote(x.Value); err == nil {
					add(text)
				}
			}
		case *ast.Ident:
			if c, isConst := info.Uses[x].(*types.Const); isConst &&
				c.Val().Kind() == constant.String && c.Parent() == c.Pkg().Scope() {
				add(constant.StringVal(c.Val()))
			}
		}
		return true
	})
	return names
}

// funcBody is a function's declaration and the type information of its
// file.
type funcBody struct {
	node ast.Node
	info *types.Info
}

// calledFuncs is the closure of what the module's functions call, through
// the module and the shared root's code, each with its body: static calls
// of functions and of methods on a concrete type; a call of an interface
// method counts as one of each method of that name on a named type of the
// interface's package implementing it (sqlc's Queries for its Querier),
// nothing wider. A lower bound: function values are not followed.
//
// Ensures: every function of the module is in it.
func calledFuncs(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
	shared *SharedRoot,
) map[*types.Func]funcBody {
	bodies, module := funcBodies(pkgs, fset, root, shared)
	called := map[*types.Func]funcBody{}
	queue := slices.Clone(module)
	for _, fn := range module {
		called[fn] = bodies[fn]
	}
	for len(queue) > 0 {
		b := called[queue[0]]
		queue = queue[1:]
		for _, target := range callTargets(b) {
			target = target.Origin()
			if tb, ok := bodies[target]; ok {
				if _, seen := called[target]; !seen {
					called[target] = tb
					queue = append(queue, target)
				}
			}
		}
	}
	return called
}

// funcBodies indexes the bodies of the module's functions and the shared
// root's, and lists the module's.
func funcBodies(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
	shared *SharedRoot,
) (map[*types.Func]funcBody, []*types.Func) {
	bodies := map[*types.Func]funcBody{}
	var module []*types.Func
	for obj, d := range moduleDecls(pkgs, fset, root) {
		if fn, isFn := obj.(*types.Func); isFn {
			bodies[fn] = funcBody{node: d.node, info: d.info}
			module = append(module, fn)
		}
	}
	for obj, d := range sharedDecls(pkgs, shared) {
		if fn, isFn := obj.(*types.Func); isFn {
			bodies[fn] = funcBody(d)
		}
	}
	return bodies, module
}

// callTargets are the functions b's calls reach (see methodTargets).
func callTargets(b funcBody) []*types.Func {
	var targets []*types.Func
	ast.Inspect(b.node, func(n ast.Node) bool {
		if call, isCall := n.(*ast.CallExpr); isCall {
			if target := calledFunc(call, b.info); target != nil {
				targets = append(targets, methodTargets(target)...)
			}
		}
		return true
	})
	return targets
}

// methodTargets is fn itself, or for an interface method the methods of
// that name on the named types of the interface's package implementing it.
func methodTargets(fn *types.Func) []*types.Func {
	recv := fn.Signature().Recv()
	if recv == nil || !types.IsInterface(recv.Type()) {
		return []*types.Func{fn}
	}
	iface, isIface := recv.Type().Underlying().(*types.Interface)
	if !isIface || fn.Pkg() == nil {
		return nil
	}
	var out []*types.Func
	scope := fn.Pkg().Scope()
	for _, name := range scope.Names() {
		tn, isType := scope.Lookup(name).(*types.TypeName)
		if !isType || types.IsInterface(tn.Type()) {
			continue
		}
		ptr := types.NewPointer(tn.Type())
		if !types.Implements(tn.Type(), iface) && !types.Implements(ptr, iface) {
			continue
		}
		obj, _, _ := types.LookupFieldOrMethod(ptr, true, fn.Pkg(), fn.Name())
		if m, isFn := obj.(*types.Func); isFn {
			out = append(out, m)
		}
	}
	return out
}

// SharedTableFacts lists the shared root's SQL tables (schema: its own
// replay, see tableFacts) that the queries the module calls name (see
// calledFuncs, queryNames), re-cited under the root: a module that runs
// sqlc's generated queries from the shared code stores the same rows. A
// name counts only when the schema declares it; a bare name matches a
// schema-qualified table when exactly one table has that last part.
//
// Ensures: sorted by id; a schema table no called query names is left out.
func SharedTableFacts(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
	shared *SharedRoot,
	schema []Fact,
) []Fact {
	used := map[string]bool{}
	for _, b := range calledFuncs(pkgs, fset, root, shared) {
		for _, n := range queryNames(b.node, b.info) {
			used[n] = true
		}
	}
	bare := map[string][]string{}
	for i := range schema {
		name := strings.ToLower(schema[i].Attrs[attrName])
		_, last, found := strings.Cut(name, ".")
		if found {
			bare[last] = append(bare[last], name)
		}
	}
	for n := range used {
		if len(bare[n]) == 1 {
			used[bare[n][0]] = true
		}
	}
	var facts []Fact
	for i := range schema {
		f := schema[i]
		name := f.Attrs[attrName]
		if !used[strings.ToLower(name)] {
			continue
		}
		f.ID = "table.@" + shared.Name + "/" + name
		f.Source.Root = shared.Name
		facts = append(facts, f)
	}
	slices.SortFunc(facts, func(a, b Fact) int { return strings.Compare(a.ID, b.ID) })
	return facts
}
