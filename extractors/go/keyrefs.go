package main

import (
	"go/ast"
	"go/types"
	"reflect"
	"slices"
	"strings"
)

// isKeyBuilder reports whether a function named name builds a Datastore
// key, whatever package declares it (cloud.google.com/go/datastore,
// webapp's wrapper): its first argument names the kind.
func isKeyBuilder(name string) bool {
	switch name {
	case "NameKey", "IDKey", "IncompleteKey":
		return true
	}
	return false
}

// keyReferences lists, per model type, the references its key fields hold:
// a field of type *Key assigned, in files, a key built from a kind's
// variable, "column:kind:null" sorted. The key is built in place
// (NameKey(V, …), NameKey(V.Value, …), IDKey, IncompleteKey) or by a
// function of files returning such a key. A reference, never ownership:
// the code shows the link, not that one entity cannot exist without the
// other. A slice of keys is left out: a reference has no multiplicity.
func keyReferences(files []codeFile, kinds []kind) map[*types.TypeName][]string {
	byVar := make(map[*types.Var]*kind, len(kinds))
	for i := range kinds {
		byVar[kinds[i].variable] = &kinds[i]
	}
	r := keyRefs{
		byVar:    byVar,
		builders: map[*types.Func]*kind{},
		refs:     map[*types.TypeName][]string{},
	}
	for i := range files {
		r.findBuilders(&files[i])
	}
	for i := range files {
		f := &files[i]
		ast.Inspect(f.syntax, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for j, lhs := range x.Lhs {
					if j < len(x.Rhs) && len(x.Lhs) == len(x.Rhs) {
						r.assigned(f.info, lhs, x.Rhs[j])
					}
				}
			case *ast.CompositeLit:
				r.literal(f.info, x)
			}
			return true
		})
	}
	for model := range r.refs {
		slices.Sort(r.refs[model])
		r.refs[model] = slices.Compact(r.refs[model])
	}
	return r.refs
}

// keyRefs accumulates the references keyReferences finds.
type keyRefs struct {
	byVar    map[*types.Var]*kind
	builders map[*types.Func]*kind
	refs     map[*types.TypeName][]string
}

// findBuilders records the functions of f returning a key built in place.
func (r *keyRefs) findBuilders(f *codeFile) {
	for _, decl := range f.syntax.Decls {
		fd, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || fd.Body == nil {
			continue
		}
		fn, isFn := f.info.Defs[fd.Name].(*types.Func)
		if !isFn {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ret, isRet := n.(*ast.ReturnStmt)
			if !isRet {
				return true
			}
			for _, result := range ret.Results {
				if k := r.builtKind(f.info, result); k != nil {
					r.builders[fn] = k
				}
			}
			return true
		})
	}
}

// builtKind is the kind of the key expr builds in place, nil for any
// other expression.
func (r *keyRefs) builtKind(info *types.Info, expr ast.Expr) *kind {
	call, isCall := ast.Unparen(expr).(*ast.CallExpr)
	if !isCall || len(call.Args) == 0 {
		return nil
	}
	if fn := calledFunc(call, info); fn == nil || !isKeyBuilder(fn.Name()) {
		return nil
	}
	arg := ast.Unparen(call.Args[0])
	if sel, isSel := arg.(*ast.SelectorExpr); isSel {
		if k := r.kindVar(info, sel.X); k != nil { // V.Value
			return k
		}
	}
	return r.kindVar(info, arg)
}

// kindVar is the kind whose variable expr names, nil for any other.
func (r *keyRefs) kindVar(info *types.Info, expr ast.Expr) *kind {
	var id *ast.Ident
	switch x := ast.Unparen(expr).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return nil
	}
	v, _ := info.Uses[id].(*types.Var)
	return r.byVar[v]
}

// keyKind is the kind of the key expr holds: built in place or by a
// builder function; nil when unknown.
func (r *keyRefs) keyKind(info *types.Info, expr ast.Expr) *kind {
	if k := r.builtKind(info, expr); k != nil {
		return k
	}
	if call, isCall := ast.Unparen(expr).(*ast.CallExpr); isCall {
		if fn := calledFunc(call, info); fn != nil {
			return r.builders[fn]
		}
	}
	return nil
}

// assigned records `x.F = value` when F is a key field and value a key of
// a known kind.
func (r *keyRefs) assigned(info *types.Info, lhs, value ast.Expr) {
	sel, isSel := ast.Unparen(lhs).(*ast.SelectorExpr)
	if !isSel {
		return
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal {
		return
	}
	if owner, field := fieldOwner(s); owner != nil {
		r.record(owner, field, r.keyKind(info, value))
	}
}

// literal records `T{F: value}` for each key field F of T set to a key of
// a known kind.
func (r *keyRefs) literal(info *types.Info, lit *ast.CompositeLit) {
	owner := namedOf(info.TypeOf(lit))
	if owner == nil {
		return
	}
	for _, e := range lit.Elts {
		kv, isKV := e.(*ast.KeyValueExpr)
		if !isKV {
			continue
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent {
			continue
		}
		if field, isVar := info.Uses[key].(*types.Var); isVar && field.IsField() {
			r.record(owner, field, r.keyKind(info, kv.Value))
		}
	}
}

// record adds the reference of owner's field to k, when field is a single
// key (*Key) and k is known and another kind than owner's: a key of its
// own kind is the entity's identity, not a link.
func (r *keyRefs) record(owner *types.Named, field *types.Var, k *kind) {
	model := owner.Origin().Obj()
	if k == nil || k.model == model || !isKeyPointer(field.Type()) {
		return
	}
	r.refs[model] = append(r.refs[model], columnOf(owner, field)+":"+k.name+":null")
}

// isKeyPointer reports whether t is a pointer to a named Key type.
func isKeyPointer(t types.Type) bool {
	p, isPointer := types.Unalias(t).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(p.Elem()).(*types.Named)
	return isNamed && named.Obj().Name() == "Key"
}

// columnOf is the column a field of owner is stored under: its datastore
// tag's name, else its own.
func columnOf(owner *types.Named, field *types.Var) string {
	st, isStruct := owner.Underlying().(*types.Struct)
	if !isStruct {
		return field.Name()
	}
	for i := range st.NumFields() {
		if st.Field(i) == field {
			name, _, _ := strings.Cut(reflect.StructTag(st.Tag(i)).Get("datastore"), ",")
			if name != "" && name != "-" {
				return name
			}
		}
	}
	return field.Name()
}
