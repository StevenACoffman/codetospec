package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// What a function reads, for the policy inventory: codetospec joins these
// to the access checks the webapp extractor names, and sorts each check's
// inputs into kinds (a stored field, a capability, a flag, a query).
const (
	kindReads     = "reads"      // codetospec's extract.KindReads
	kindUsesValue = "uses_value" // codetospec's extract.KindUsesValue
	kindLoads     = "loads"      // codetospec's extract.KindLoads

	attrField         = "field"          // a read field, "pkg.Type.Field"
	attrType          = "type"           // a value's declared type, "pkg.Type"
	attrValue         = "value"          // a value's literal value, when it has one
	attrDatastoreKind = "datastore_kind" // a loaded datastore kind
	attrProperties    = "properties"     // the properties a load filters by, comma-separated
)

// inputCaller is a definition whose code reads inputs: a function or method
// with a body, or a package-level variable holding a function.
type inputCaller struct {
	decl moduleDecl
	body ast.Node
}

// inputCallers lists the module's definitions with code, skipping generated
// files: their code yields no rules.
func inputCallers(pkgs []*packages.Package, fset *token.FileSet, root string) []inputCaller {
	generated := generatedFiles(pkgs, fset, root)
	decls := moduleDecls(pkgs, fset, root)
	for obj, v := range callableVars(pkgs, fset, root) {
		decls[obj] = v
	}
	var callers []inputCaller
	for _, d := range decls {
		if body := callerBody(d.node); body != nil && !generated[d.file] {
			callers = append(callers, inputCaller{decl: d, body: body})
		}
	}
	return callers
}

// generatedFiles names the module's files a tool wrote.
func generatedFiles(pkgs []*packages.Package, fset *token.FileSet, root string) map[string]bool {
	generated := map[string]bool{}
	for _, f := range moduleFiles(pkgs, fset, root) {
		if ast.IsGenerated(f.syntax) {
			generated[f.rel] = true
		}
	}
	return generated
}

// ReadFacts lists, per function, the fields of the module's struct types it
// reads (`udi.PrimaryRole` → models.UserDistrictInfo.PrimaryRole, whatever
// the variable is called), each named by the struct declaring it, so a
// field promoted from an embedded struct is named by its own struct. Every
// field counts, slices included: the inventory compares them with the
// registry's relations, which are mostly lists of ids.
//
// Requires: pkgs carry syntax and type information.
// Ensures:  one fact per function and field, sorted by id.
func ReadFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	module := modulePackages(pkgs)
	var facts []Fact
	for _, c := range inputCallers(pkgs, fset, root) {
		fields := map[string]bool{}
		ast.Inspect(c.body, func(n ast.Node) bool {
			sel, isSel := n.(*ast.SelectorExpr)
			if !isSel {
				return true
			}
			if name, ok := readField(c.decl.info.Selections[sel], module); ok {
				fields[name] = true
			}
			return true
		})
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			facts = append(facts, inputFact(kindReads, &c.decl, fset, name,
				map[string]string{attrField: name}))
		}
	}
	sortFacts(facts)
	return facts
}

// readField names the field a selection reads, "pkg.Type.Field", when it is
// a field of a struct type the module declares.
func readField(sel *types.Selection, module map[string]bool) (string, bool) {
	if sel == nil || sel.Kind() != types.FieldVal {
		return "", false
	}
	owner, field := fieldOwner(sel)
	if owner == nil || owner.Obj().Pkg() == nil || !module[owner.Obj().Pkg().Path()] {
		return "", false
	}
	return owner.Obj().Pkg().Name() + "." + owner.Obj().Name() + "." + field.Name(), true
}

// fieldOwner follows a field selection through its embedded fields to the
// named struct type that declares the field.
func fieldOwner(sel *types.Selection) (*types.Named, *types.Var) {
	t := sel.Recv()
	for i, index := range sel.Index() {
		named := namedOf(t)
		if named == nil {
			return nil, nil
		}
		st, isStruct := named.Underlying().(*types.Struct)
		if !isStruct || index >= st.NumFields() {
			return nil, nil
		}
		if i == len(sel.Index())-1 {
			return named, st.Field(index)
		}
		t = st.Field(index).Type()
	}
	return nil, nil
}

// namedOf is the named type behind t, through a pointer and aliases.
func namedOf(t types.Type) *types.Named {
	t = types.Unalias(t)
	if p, isPointer := t.(*types.Pointer); isPointer {
		t = types.Unalias(p.Elem())
	}
	named, _ := t.(*types.Named)
	return named
}

// ValueFacts lists, per function, the package-level constants of the module
// it uses, and its package-level variables of a basic type (a string, a
// number, a boolean), each with its declared type and, when the code
// writes one, its literal value. A capability (`capabilities.CanViewCDI`,
// a generated variable) and a feature flag (a constant) are recognized by
// their type; a variable holding data or a function is left out.
//
// Requires: as ReadFacts.
// Ensures:  one fact per function and value, sorted by id.
func ValueFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	module := modulePackages(pkgs)
	literals := varLiterals(pkgs, fset, root)
	var facts []Fact
	for _, c := range inputCallers(pkgs, fset, root) {
		values := map[string]map[string]string{}
		ast.Inspect(c.body, func(n ast.Node) bool {
			id, isIdent := n.(*ast.Ident)
			if !isIdent {
				return true
			}
			if name, attrs, ok := usedValue(c.decl.info.Uses[id], module, literals, fset); ok {
				values[name] = attrs
			}
			return true
		})
		for _, name := range slices.Sorted(maps.Keys(values)) {
			facts = append(facts, inputFact(kindUsesValue, &c.decl, fset, name, values[name]))
		}
	}
	sortFacts(facts)
	return facts
}

// usedValue describes obj when it is a value ValueFacts reports: its name
// as "pkg.Name" and its attributes.
func usedValue(
	obj types.Object,
	module map[string]bool,
	literals map[token.Position]string,
	fset *token.FileSet,
) (string, map[string]string, bool) {
	if obj == nil || obj.Pkg() == nil || !module[obj.Pkg().Path()] ||
		obj.Parent() != obj.Pkg().Scope() {
		return "", nil, false
	}
	attrs := map[string]string{attrType: types.TypeString(
		types.Default(obj.Type()), func(p *types.Package) string { return p.Name() })}
	switch o := obj.(type) {
	case *types.Const:
		attrs[attrValue] = constantText(o.Val())
	case *types.Var:
		basic, isBasic := o.Type().Underlying().(*types.Basic)
		if !isBasic || basic.Info()&(types.IsBoolean|types.IsNumeric|types.IsString) == 0 {
			return "", nil, false
		}
		if v, ok := literals[fset.Position(o.Pos())]; ok {
			attrs[attrValue] = v
		}
	default:
		return "", nil, false
	}
	name := obj.Pkg().Name() + "." + obj.Name()
	attrs[attrName] = name
	return name, attrs, true
}

// constantText is a constant's value as text: a string's contents, any
// other value as Go writes it.
func constantText(v constant.Value) string {
	if v.Kind() == constant.String {
		return constant.StringVal(v)
	}
	return v.ExactString()
}

// varLiterals maps each package-level variable initialized by a basic
// literal to that literal's value, by its name's position: generated files
// included, since a service's capability variables are generated.
func varLiterals(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
) map[token.Position]string {
	literals := map[token.Position]string{}
	for _, v := range packageVarsOf(pkgs, fset, root) {
		for i, name := range v.spec.Names {
			if i >= len(v.spec.Values) || v.info.Defs[name] != v.obj {
				continue
			}
			if lit, isLit := v.spec.Values[i].(*ast.BasicLit); isLit {
				literals[fset.Position(v.obj.Pos())] = literalText(lit)
			}
		}
	}
	return literals
}

// literalText is a basic literal's value: a string unquoted, anything else
// as written.
func literalText(lit *ast.BasicLit) string {
	if lit.Kind == token.STRING {
		if s, err := strconv.Unquote(lit.Value); err == nil {
			return s
		}
	}
	return lit.Value
}

// LoadFacts lists, per function, the datastore kinds it queries
// (`datastore.NewQuery(models.UserDistrictInfoKind)`, by the constant's
// value) and the properties its queries filter by (`FilterField("kaid",
// …)`, `Filter("kaid =", …)`): an existence or relation lookup reads no
// struct field, so ReadFacts can't see it. The names are Cloud
// Datastore's, which webapp's wrapper keeps.
//
// Requires: as ReadFacts.
// Ensures:  one fact per function and kind, sorted by id; the properties
// are those of every query of the function (a function rarely runs two).
func LoadFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	var facts []Fact
	for _, c := range inputCallers(pkgs, fset, root) {
		kinds, properties := queriesOf(c.body, c.decl.info)
		for _, kind := range kinds {
			facts = append(facts, inputFact(kindLoads, &c.decl, fset, kind, map[string]string{
				attrDatastoreKind: kind, attrProperties: strings.Join(properties, ","),
			}))
		}
	}
	sortFacts(facts)
	return facts
}

// queriesOf reads the datastore queries body runs: the kinds it builds
// queries on and the properties it filters by, each sorted and unique.
func queriesOf(body ast.Node, info *types.Info) ([]string, []string) {
	kinds, properties := map[string]bool{}, map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall || len(call.Args) == 0 {
			return true
		}
		switch datastoreCall(call, info) {
		case "NewQuery":
			kinds[argText(call.Args[0], info)] = true
		case "FilterField", "Filter":
			if p, ok := stringArg(call.Args[0], info); ok {
				field, _, _ := strings.Cut(strings.TrimSpace(p), " ")
				properties[field] = true
			}
		}
		return true
	})
	return slices.Sorted(maps.Keys(kinds)), slices.Sorted(maps.Keys(properties))
}

// datastoreCall names the datastore function or Query method call calls:
// NewQuery of a package named datastore, FilterField or Filter of its Query
// type; "" for anything else.
func datastoreCall(call *ast.CallExpr, info *types.Info) string {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return ""
	}
	fn, isFunc := info.Uses[sel.Sel].(*types.Func)
	if !isFunc || fn.Pkg() == nil || fn.Pkg().Name() != "datastore" {
		return ""
	}
	return fn.Name()
}

// argText is an argument's constant value when it has one (a kind
// constant), else its source.
func argText(e ast.Expr, info *types.Info) string {
	if s, ok := stringArg(e, info); ok {
		return s
	}
	return types.ExprString(e)
}

// stringArg is a string constant argument's value.
func stringArg(e ast.Expr, info *types.Info) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// inputFact is one input fact of kind about caller, keyed by key.
func inputFact(
	kind string,
	caller *moduleDecl,
	fset *token.FileSet,
	key string,
	attrs map[string]string,
) Fact {
	lines := caller.lines(fset)
	return Fact{
		Kind:      kind,
		ID:        kind + "." + caller.file + ":" + lines + "#" + key,
		Attrs:     attrs,
		Source:    Ref{Path: caller.file, Lines: lines},
		Origin:    origin,
		Certainty: certain,
	}
}

// modulePackages names the module's packages by import path.
func modulePackages(pkgs []*packages.Package) map[string]bool {
	module := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		module[p.PkgPath] = true
	}
	return module
}

// sortFacts orders facts by id.
func sortFacts(facts []Fact) {
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
}
