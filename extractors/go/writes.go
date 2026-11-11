package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/packages"
)

// What a function writes of the module's state, for the architecture rules
// codetospec proposes to anarch-lint (who may write each stored state
// field).
const (
	kindWrites       = "writes"        // codetospec's extract.KindWrites
	attrFieldPackage = "field_package" // the import path of a written field's struct
)

// writtenField is one field a function writes: its struct's import path
// and its enumeration's fact id.
type writtenField struct {
	pkgPath, enum string
}

// WriteFacts lists, per function, the fields of the module's struct types
// that hold one of its enumerations (EnumFacts) and that the function
// writes: an assignment, an increment, or a keyed composite literal setting
// the field, which is what anarch-lint's fields rules count as a write.
// Each is named as ReadFacts names a field (`pkg.Type.Field`), with the
// struct's import path and the enumeration's fact id. A method writing a
// field of its own receiver's type is left out, as anarch-lint leaves it
// out: the type keeps its state to itself.
//
// Requires: pkgs carry syntax and type information.
// Ensures:  one fact per function and field, sorted by id.
func WriteFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	enums := moduleEnums(pkgs, fset, root)
	module := modulePackages(pkgs)
	var facts []Fact
	for _, c := range inputCallers(pkgs, fset, root) {
		w := fieldWriter{
			info: c.decl.info, enums: enums, module: module,
			receiver: receiverType(c.decl.obj), fields: map[string]writtenField{},
		}
		ast.Inspect(c.body, w.visit)
		for _, name := range slices.Sorted(maps.Keys(w.fields)) {
			f := w.fields[name]
			facts = append(facts, inputFact(kindWrites, &c.decl, fset, name, map[string]string{
				attrField: name, attrFieldPackage: f.pkgPath, attrEnum: f.enum,
			}))
		}
	}
	sortFacts(facts)
	return facts
}

// fieldWriter collects the enum-typed fields one function writes.
type fieldWriter struct {
	info     *types.Info
	enums    map[*types.TypeName]*enumType
	module   map[string]bool
	receiver *types.TypeName // the method's receiver type; nil for a function
	fields   map[string]writtenField
}

// visit records the writes of node n.
func (w *fieldWriter) visit(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.AssignStmt:
		for _, lhs := range x.Lhs {
			w.selected(lhs)
		}
	case *ast.IncDecStmt:
		w.selected(x.X)
	case *ast.CompositeLit:
		w.literal(x)
	}
	return true
}

// selected records expr when it selects a field (`o.Status`).
func (w *fieldWriter) selected(expr ast.Expr) {
	sel, isSel := ast.Unparen(expr).(*ast.SelectorExpr)
	if !isSel {
		return
	}
	s := w.info.Selections[sel]
	if s == nil || s.Kind() != types.FieldVal {
		return
	}
	if owner, field := fieldOwner(s); owner != nil {
		w.add(owner, field)
	}
}

// literal records the fields a keyed composite literal of a struct sets.
func (w *fieldWriter) literal(lit *ast.CompositeLit) {
	owner := namedOf(w.info.TypeOf(lit))
	if owner == nil {
		return
	}
	if _, isStruct := owner.Underlying().(*types.Struct); !isStruct {
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
		if field, isVar := w.info.Uses[key].(*types.Var); isVar && field.IsField() {
			w.add(owner, field)
		}
	}
}

// add records field of owner when owner is a module struct other than the
// method's receiver type and the field holds a module enumeration.
func (w *fieldWriter) add(owner *types.Named, field *types.Var) {
	obj := owner.Origin().Obj()
	if obj.Pkg() == nil || !w.module[obj.Pkg().Path()] || obj == w.receiver {
		return
	}
	typ := namedOf(field.Type())
	if typ == nil {
		return
	}
	e := w.enums[typ.Origin().Obj()]
	if e == nil {
		return
	}
	name := obj.Pkg().Name() + "." + obj.Name() + "." + field.Name()
	w.fields[name] = writtenField{pkgPath: obj.Pkg().Path(), enum: e.id}
}

// receiverType is the named type a method is declared on, nil for a
// function or a variable.
func receiverType(obj types.Object) *types.TypeName {
	fn, isFunc := obj.(*types.Func)
	if !isFunc {
		return nil
	}
	recv := fn.Signature().Recv()
	if recv == nil {
		return nil
	}
	if named := namedOf(recv.Type()); named != nil {
		return named.Origin().Obj()
	}
	return nil
}
