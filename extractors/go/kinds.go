package main

import (
	"cmp"
	"encoding/json"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// The table fact kind, and its columns and stored-by attributes
// (extract.KindTable, extract.AttrColumns, extract.AttrStoredBy).
const (
	kindTable    = "table"
	attrColumns  = "columns"
	attrStoredBy = "stored_by"
	// kindSuffix ends the name of a package variable naming a kind, after
	// the model type it stores: KhanmigoDistrictJobKind.
	kindSuffix = "Kind"
)

// kind is one Datastore kind the code declares: a package variable
// naming it, and the struct type stored under it.
type kind struct {
	name     string // the kind's name, as Datastore stores it
	variable *types.Var
	model    *types.TypeName
}

// codeFile is a file of loaded code: the analyzed module's, or a shared
// root's (root set), its path relative to that tree.
type codeFile struct {
	syntax    *ast.File
	info      *types.Info
	rel, root string
}

// cite is node's lines in f.
func (f *codeFile) cite(fset *token.FileSet, node ast.Node) Ref {
	r := lineSpan(fset, node, f.rel)
	r.Root = f.root
	return r
}

// KindFacts lists a table fact per Datastore kind the module declares (see
// kindOf): the kind's name, the columns its model stores, the model's doc
// comment and its fields', the model when named otherwise (stored_by), the
// references its key fields hold (see keyReferences), citing the model's
// declaration.
//
// Requires: pkgs are the module's packages, with syntax and type information.
// Ensures:  sorted by id; columns in field order.
func KindFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	var files []codeFile
	for _, f := range moduleFiles(pkgs, fset, root) {
		files = append(files, codeFile{syntax: f.syntax, info: f.info, rel: f.rel})
	}
	return kindFacts(files, files, fset, "table.kind.", func(*kind) bool { return true })
}

// SharedKindFacts lists the table facts (see KindFacts) of the shared
// root's kinds the analyzed module reaches (see ReachFacts), by their
// variable or their model type, cited under the shared root: a module whose
// models live in the shared code stores the same entities. References come
// from the shared code and the module's alike.
//
// Requires: as ReachFacts.
// Ensures:  sorted by id.
func SharedKindFacts(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
	shared *SharedRoot,
) []Fact {
	files := sharedFiles(pkgs, fset, shared)
	var module []codeFile
	for _, f := range moduleFiles(pkgs, fset, root) {
		module = append(module, codeFile{syntax: f.syntax, info: f.info, rel: f.rel})
	}
	_, reached := reachedDecls(pkgs, shared)
	used := func(k *kind) bool { return reached[k.variable] || reached[k.model] }
	return kindFacts(files, append(module, files...), fset,
		"table.kind.@"+shared.Name+"/", used)
}

// kindFacts lists the table facts of the kinds files declare that keep
// selects, their references read from the code of refFiles, ids prefixed.
func kindFacts(
	files, refFiles []codeFile,
	fset *token.FileSet,
	prefix string,
	keep func(*kind) bool,
) []Fact {
	kinds, specs := declaredKinds(files)
	refs := keyReferences(refFiles, kinds)
	var facts []Fact
	for i := range kinds {
		k := &kinds[i]
		spec, ok := specs[k.model]
		if !ok || !keep(k) {
			continue
		}
		facts = append(facts, kindFact(k, spec, fset, prefix, refs[k.model]))
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// modelSpec is a model type's declaration and the file declaring it.
type modelSpec struct {
	gen  *ast.GenDecl
	spec *ast.TypeSpec
	file *codeFile
}

// declaredKinds finds the kinds files declare, in file order, and the
// declarations of the struct types they could name.
func declaredKinds(files []codeFile) ([]kind, map[*types.TypeName]modelSpec) {
	var kinds []kind
	specs := map[*types.TypeName]modelSpec{}
	for i := range files {
		f := &files[i]
		for _, decl := range f.syntax.Decls {
			gen, isGen := decl.(*ast.GenDecl)
			if !isGen {
				continue
			}
			for _, s := range gen.Specs {
				switch spec := s.(type) {
				case *ast.TypeSpec:
					if tn, isType := f.info.Defs[spec.Name].(*types.TypeName); isType {
						specs[tn] = modelSpec{gen: gen, spec: spec, file: f}
					}
				case *ast.ValueSpec:
					kinds = append(kinds, valueKinds(spec, f.info)...)
				}
			}
		}
	}
	return kinds, specs
}

// valueKinds are the kinds the variables of spec name.
func valueKinds(spec *ast.ValueSpec, info *types.Info) []kind {
	var kinds []kind
	for i, name := range spec.Names {
		v, isVar := info.Defs[name].(*types.Var)
		if !isVar || i >= len(spec.Values) || v.Parent() != v.Pkg().Scope() {
			continue
		}
		if k, ok := kindOf(v, spec.Values[i], info); ok {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

// kindOf recognizes a package variable naming a Datastore kind: its type
// is a struct named Kind holding one string, and its value is
//
//   - RegisterKind(k, new(T)) (any package's, RegisterNonBackupKind too):
//     the kind is k's constant value, a literal or a named constant, and
//     the model T;
//   - Kind{k}: the kind is k's constant value, and the model the struct of
//     the variable's package named like the variable without its "Kind"
//     suffix (KhanmigoDistrictJobKind stores KhanmigoDistrictJob).
func kindOf(v *types.Var, value ast.Expr, info *types.Info) (kind, bool) {
	if !isKindType(v.Type()) {
		return kind{}, false
	}
	var name string
	var model *types.TypeName
	switch x := ast.Unparen(value).(type) {
	case *ast.CallExpr:
		fn := calledFunc(x, info)
		if fn == nil || len(x.Args) != 2 || !strings.HasPrefix(fn.Name(), "Register") {
			return kind{}, false
		}
		name, model = constString(x.Args[0], info), newOf(x.Args[1], info)
	case *ast.CompositeLit:
		if len(x.Elts) != 1 {
			return kind{}, false
		}
		elt := x.Elts[0]
		if kv, isKV := elt.(*ast.KeyValueExpr); isKV {
			elt = kv.Value
		}
		name = constString(elt, info)
		if tn, isType := v.Pkg().Scope().Lookup(
			strings.TrimSuffix(v.Name(), kindSuffix)).(*types.TypeName); isType {
			model = tn
		}
	}
	if name == "" || model == nil || !isStruct(model) {
		return kind{}, false
	}
	return kind{name: name, variable: v, model: model}, true
}

// isKindType reports whether t is a struct type named Kind holding one
// string: the kind wrapper of webapp and districts-jobs alike.
func isKindType(t types.Type) bool {
	named, isNamed := types.Unalias(t).(*types.Named)
	if !isNamed || named.Obj().Name() != "Kind" {
		return false
	}
	st, isStruct := named.Underlying().(*types.Struct)
	if !isStruct || st.NumFields() != 1 {
		return false
	}
	basic, isBasic := st.Field(0).Type().Underlying().(*types.Basic)
	return isBasic && basic.Info()&types.IsString != 0
}

// calledFunc is the function call calls, nil for anything else.
func calledFunc(call *ast.CallExpr, info *types.Info) *types.Func {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return nil
	}
	fn, _ := info.Uses[id].(*types.Func)
	return fn
}

// constString is expr's constant string value, "" when it has none.
func constString(expr ast.Expr, info *types.Info) string {
	tv, ok := info.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return ""
	}
	return constant.StringVal(tv.Value)
}

// newOf is the named type T of new(T), nil for anything else.
func newOf(expr ast.Expr, info *types.Info) *types.TypeName {
	call, isCall := ast.Unparen(expr).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 {
		return nil
	}
	if id, isIdent := ast.Unparen(call.Fun).(*ast.Ident); !isIdent || id.Name != "new" {
		return nil
	}
	if named := namedOf(info.TypeOf(call.Args[0])); named != nil {
		return named.Obj()
	}
	return nil
}

// isStruct reports whether tn names a struct type.
func isStruct(tn *types.TypeName) bool {
	_, ok := tn.Type().Underlying().(*types.Struct)
	return ok
}

// kindFact is k's table fact, declared by m, holding refs.
func kindFact(k *kind, m modelSpec, fset *token.FileSet, prefix string, refs []string) Fact {
	st, _ := m.spec.Type.(*ast.StructType)
	columns, descriptions := storedColumns(st, m.file.info)
	attrs := map[string]string{attrName: k.name, attrColumns: strings.Join(columns, ", ")}
	if doc := docOf(m.spec.Doc, m.gen, len(m.gen.Specs) == 1); doc != "" {
		attrs[attrDescription] = doc
	}
	if k.model.Name() != k.name {
		attrs[attrStoredBy] = k.model.Name()
	}
	if len(descriptions) > 0 {
		if data, err := json.Marshal(descriptions); err == nil { // sorted keys
			attrs[attrColumnDescriptions] = string(data)
		}
	}
	if len(refs) > 0 {
		attrs[attrReferences] = strings.Join(refs, ", ")
	}
	return Fact{
		Kind: kindTable, ID: prefix + k.name, Attrs: attrs,
		Source: m.file.cite(fset, m.spec), Origin: origin, Certainty: certain,
	}
}

// storedColumns lists the fields Datastore stores, "column:type": the
// `datastore:"name"` tag's name, else the exported field's; a field the
// tag skips ("-"), an unexported field and an embedded one (its columns
// are its own type's) are left out. It also returns their doc comments,
// else their line comments, by column, each on one line.
func storedColumns(st *ast.StructType, info *types.Info) ([]string, map[string]string) {
	var columns []string
	descriptions := map[string]string{}
	if st == nil {
		return nil, descriptions
	}
	for _, field := range st.Fields.List {
		tag := storedName(field)
		if tag == "-" || len(field.Names) == 0 {
			continue
		}
		typ := types.TypeString(info.TypeOf(field.Type), func(p *types.Package) string {
			return p.Name()
		})
		doc := docOf(field.Doc, nil, false)
		if doc == "" {
			doc = docOf(field.Comment, nil, false)
		}
		for _, id := range field.Names {
			if !id.IsExported() {
				continue
			}
			column := cmp.Or(tag, id.Name)
			columns = append(columns, column+":"+typ)
			if doc != "" {
				descriptions[column] = doc
			}
		}
	}
	return columns, descriptions
}

// storedName is the name a field's `datastore` tag gives it; "" when none.
func storedName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return ""
	}
	name, _, _ := strings.Cut(reflect.StructTag(tag).Get("datastore"), ",")
	return name
}

// docOf is a comment group on one line; when it is nil and orGen, the
// declaration's own.
func docOf(group *ast.CommentGroup, gen *ast.GenDecl, orGen bool) string {
	if group == nil && orGen && gen != nil {
		group = gen.Doc
	}
	if group == nil {
		return ""
	}
	return strings.Join(strings.Fields(group.Text()), " ")
}

// sharedFiles are the files of the shared root's packages among pkgs and
// their dependencies, relative to the shared root.
func sharedFiles(pkgs []*packages.Package, fset *token.FileSet, shared *SharedRoot) []codeFile {
	var files []codeFile
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.TypesInfo == nil || !shared.holds(p.PkgPath) {
			return
		}
		for _, file := range p.Syntax {
			rel, err := relTo(shared.Dir, fset.Position(file.Pos()).Filename)
			if err == nil && !strings.HasPrefix(rel, "..") {
				files = append(
					files,
					codeFile{syntax: file, info: p.TypesInfo, rel: rel, root: shared.Name},
				)
			}
		}
	})
	return files
}
