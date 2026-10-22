package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// enumType is a named string or integer type of the module with its
// constants: Go's enumeration idiom.
type enumType struct {
	obj    *types.TypeName
	id     string // the enum fact's id
	name   string // pkg.Type
	at     Ref
	values []enumValue
}

// enumValue is one constant of an enumType.
type enumValue struct {
	name, value string
	at          Ref
}

// The enum fact kind, and the attribute naming an enum fact from a value
// or a transition.
const (
	kindEnum = "enum"
	attrEnum = "enum"
)

// minEnumValues is how many constants make a named type an enumeration:
// one constant is a default or a sentinel, not a set of states.
const minEnumValues = 2

// EnumFacts lists the module's enumerations — named string or integer
// types, outside generated files, with at least minEnumValues constants
// of the type — as one enum
// fact (citing the type) and one enum_value fact per constant, then the
// transitions between their values the code guards (see transitionFacts).
//
// Requires: pkgs are the module's packages, with syntax and type information.
// Ensures:  sorted by id; values in declaration order within an enum.
func EnumFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	enums := moduleEnums(pkgs, fset, root)
	var facts []Fact
	for _, e := range enums {
		facts = append(facts, Fact{
			Kind: kindEnum, ID: e.id,
			Attrs:  map[string]string{attrName: e.name},
			Source: e.at, Origin: origin, Certainty: certain,
		})
		for _, v := range e.values {
			facts = append(facts, Fact{
				Kind: "enum_value",
				ID:   "enum_value." + v.at.Path + "#" + v.name,
				Attrs: map[string]string{
					attrEnum: e.id, attrName: v.name, "value": v.value,
				},
				Source: v.at, Origin: origin, Certainty: certain,
			})
		}
	}
	facts = append(facts, transitionFacts(pkgs, fset, root, enums)...)
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// moduleFile is one file of the module's own packages, with its types.
type moduleFile struct {
	syntax *ast.File
	rel    string // relative to the module root
	info   *types.Info
}

// moduleFiles lists the type-checked files of pkgs under root.
func moduleFiles(pkgs []*packages.Package, fset *token.FileSet, root string) []moduleFile {
	var files []moduleFile
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			rel, err := relTo(root, fset.Position(file.Pos()).Filename)
			if err == nil && !strings.HasPrefix(rel, "..") {
				files = append(files, moduleFile{syntax: file, rel: rel, info: p.TypesInfo})
			}
		}
	}
	return files
}

// moduleEnums finds the module's enumerations, keyed by type.
func moduleEnums(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
) map[*types.TypeName]*enumType {
	enums := map[*types.TypeName]*enumType{}
	var consts []*types.Const
	for _, f := range moduleFiles(pkgs, fset, root) {
		// A generated file's enumerations mirror an outside schema (an
		// API client's statuses), not the module's own states; its code
		// yields no rules either.
		if !ast.IsGenerated(f.syntax) {
			consts = append(consts, declareEnums(&f, fset, enums)...)
		}
	}
	for _, c := range consts {
		if e := enumOf(c.Type(), enums); e != nil {
			e.values = append(e.values, enumValue{
				name:  c.Name(),
				value: constValue(c.Val()),
				at:    declaredAt(c, fset, root),
			})
		}
	}
	for obj, e := range enums {
		if len(e.values) < minEnumValues {
			delete(enums, obj)
			continue
		}
		sort.SliceStable(e.values, func(i, j int) bool {
			a, b := e.values[i].at, e.values[j].at
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			return lineOf(a) < lineOf(b)
		})
	}
	return enums
}

// declareEnums adds to enums the enumeration bases f declares, and returns
// the constants it declares, whose types are known once every file is.
func declareEnums(
	f *moduleFile,
	fset *token.FileSet,
	enums map[*types.TypeName]*enumType,
) []*types.Const {
	var consts []*types.Const
	for _, decl := range f.syntax.Decls {
		for _, d := range declared(decl) {
			switch obj := f.info.Defs[d.name].(type) {
			case *types.TypeName:
				if isEnumBase(obj) {
					enums[obj] = &enumType{
						obj:  obj,
						id:   kindEnum + "." + f.rel + "#" + obj.Name(),
						name: obj.Pkg().Name() + "." + obj.Name(),
						at:   lineSpan(fset, d.node, f.rel),
					}
				}
			case *types.Const:
				consts = append(consts, obj)
			}
		}
	}
	return consts
}

// isEnumBase reports whether a named type is a defined string or integer
// type, the bases of Go's enumeration idiom.
func isEnumBase(obj *types.TypeName) bool {
	if obj.IsAlias() {
		return false
	}
	basic, isBasic := obj.Type().Underlying().(*types.Basic)
	return isBasic && basic.Info()&(types.IsString|types.IsInteger) != 0
}

// constValue renders a constant as written: a string unquoted, a number
// exactly.
func constValue(v constant.Value) string {
	if v.Kind() == constant.String {
		return constant.StringVal(v)
	}
	return v.ExactString()
}

// lineSpan cites a node's lines in file rel.
func lineSpan(fset *token.FileSet, node ast.Node, rel string) Ref {
	return Ref{
		Path:  rel,
		Lines: itoa(fset.Position(node.Pos()).Line) + "-" + itoa(fset.Position(node.End()).Line),
	}
}

// lineOf is the first line of a ref, for ordering; 0 when it has none.
func lineOf(r Ref) int {
	start, _, _ := strings.Cut(r.Lines, "-")
	n, _ := strconv.Atoi(start)
	return n
}
