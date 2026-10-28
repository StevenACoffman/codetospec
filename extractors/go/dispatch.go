package main

import (
	"go/token"
	"go/types"
	"sort"
)

// A call or test fact found through an interface carries attrDispatch =
// dispatchInterface: the caller calls a method of a module interface, and
// the callee is one of the module's implementations of it. Which one runs
// is decided at run time, so such a fact is a possible use, not a known
// one; codetospec keeps the two apart.
const (
	attrDispatch      = "dispatch"
	dispatchInterface = "interface"
)

// implementations maps the position of each method of the module's own
// interfaces to the module's methods implementing it. A call x.M() with x
// of interface type resolves to the interface's method, which no call
// fact could otherwise follow to code. Interfaces declared outside the
// module (error, io.Writer, fmt.Stringer) are not followed: err.Error()
// would reach every error type of the module. Generic interfaces and
// types, and constraint interfaces, are skipped: an uninstantiated one
// implements nothing.
//
// Requires: decls from moduleDecls.
// Ensures:  keys are interface method positions (the same in every test
// variant of a package, unlike objects); each value lists module
// declarations once, sorted by file then line.
func implementations(
	decls map[types.Object]moduleDecl,
	fset *token.FileSet,
) map[token.Position][]moduleDecl {
	module := typesOf(decls)
	byMethod := map[token.Position][]moduleDecl{}
	for _, iface := range module.ifaces {
		for _, named := range module.concrete {
			for _, m := range module.implemented(named, iface) {
				if impl, isDecl := decls[m.impl]; isDecl {
					pos := fset.Position(m.method.Pos())
					byMethod[pos] = append(byMethod[pos], impl)
				}
			}
		}
	}
	for pos, impls := range byMethod {
		byMethod[pos] = sortedDecls(impls, fset)
	}
	return byMethod
}

// moduleTypes are the module's interfaces (with at least one method) and
// its other named types, and the packages declaring them.
type moduleTypes struct {
	ifaces   []*types.Interface
	concrete []*types.Named
	own      map[*types.Package]bool
}

// typesOf sorts the named types of decls into interfaces and the rest.
func typesOf(decls map[types.Object]moduleDecl) moduleTypes {
	t := moduleTypes{own: map[*types.Package]bool{}}
	for obj := range decls {
		t.own[obj.Pkg()] = true
		named := plainNamed(obj)
		if named == nil {
			continue
		}
		iface, isIface := named.Underlying().(*types.Interface)
		switch {
		case !isIface:
			t.concrete = append(t.concrete, named)
		case iface.IsMethodSet() && iface.NumMethods() > 0:
			t.ifaces = append(t.ifaces, iface)
		}
	}
	return t
}

// implementedMethod is a method of an interface and the method
// implementing it.
type implementedMethod struct {
	method *types.Func
	impl   types.Object
}

// implemented lists, when named (or a pointer to it) implements iface,
// each method of iface the module declares with the method of named
// implementing it; methods iface embeds from outside the module are left
// out, like the interfaces they come from.
func (t *moduleTypes) implemented(named *types.Named, iface *types.Interface) []implementedMethod {
	receiver := implementer(named, iface)
	if receiver == nil {
		return nil
	}
	var methods []implementedMethod
	for i := range iface.NumMethods() {
		m := iface.Method(i)
		if t.own[m.Pkg()] {
			impl, _, _ := types.LookupFieldOrMethod(receiver, true, m.Pkg(), m.Name())
			methods = append(methods, implementedMethod{method: m, impl: impl})
		}
	}
	return methods
}

// plainNamed is the named type obj declares, nil for any other object and
// for a generic type.
func plainNamed(obj types.Object) *types.Named {
	tn, isType := obj.(*types.TypeName)
	if !isType || tn.IsAlias() {
		return nil
	}
	named, isNamed := tn.Type().(*types.Named)
	if !isNamed || named.TypeParams().Len() > 0 {
		return nil
	}
	return named
}

// implementer is named, or a pointer to it, whichever implements iface
// (named first: its method set is the smaller); nil when neither does.
func implementer(named *types.Named, iface *types.Interface) types.Type {
	for _, t := range []types.Type{named, types.NewPointer(named)} {
		if types.Implements(t, iface) {
			return t
		}
	}
	return nil
}

// sortedDecls removes repeated declarations and sorts the rest by file
// then line, so facts come out in a fixed order.
func sortedDecls(list []moduleDecl, fset *token.FileSet) []moduleDecl {
	seen := map[types.Object]bool{}
	out := list[:0]
	for _, d := range list {
		if !seen[d.obj] {
			seen[d.obj] = true
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return fset.Position(out[i].node.Pos()).Line < fset.Position(out[j].node.Pos()).Line
	})
	return out
}
