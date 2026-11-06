package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// The struct fact kind and its fields attribute (extract.KindStruct,
// extract.AttrFields).
const (
	kindStruct = "struct"
	attrFields = "fields"
	// attrTags maps each field the datastore stores to the name it stores
	// it under, "Field:name" comma-separated, every field included: a
	// query names a property, the inventory needs the field.
	attrTags = "tags"
)

// StructFacts lists the module's struct types, outside generated files,
// each with the fields a rule condition can compare or test for presence
// (see conditionable): the names the condition vocabulary offers the map,
// so a rule checking an order's status names shop.Order.Status as the code
// does. A struct without such a field is left out.
//
// Requires: pkgs are the module's packages, with syntax and type information.
// Ensures:  sorted by id; fields in declaration order.
func StructFacts(pkgs []*packages.Package, fset *token.FileSet, root string) []Fact {
	var facts []Fact
	for _, f := range moduleFiles(pkgs, fset, root) {
		// A generated file's structs mirror an outside schema, and its code
		// yields no rules.
		if ast.IsGenerated(f.syntax) {
			continue
		}
		for _, decl := range f.syntax.Decls {
			for _, d := range declared(decl) {
				if fact, ok := structFact(&f, fset, d); ok {
					facts = append(facts, fact)
				}
			}
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts
}

// structFact is the fact of the declaration d of f when it is a struct
// type with a conditionable field.
func structFact(f *moduleFile, fset *token.FileSet, d declaredName) (Fact, bool) {
	obj, isType := f.info.Defs[d.name].(*types.TypeName)
	if !isType {
		return Fact{}, false
	}
	return newStructFact(kindStruct+"."+f.rel+"#"+obj.Name(), obj, lineSpan(fset, d.node, f.rel))
}

// newStructFact is the fact id of the type obj declared at, when obj is a
// struct type (not an alias) with a conditionable field.
func newStructFact(id string, obj *types.TypeName, at Ref) (Fact, bool) {
	if obj.IsAlias() {
		return Fact{}, false
	}
	st, isStruct := obj.Type().Underlying().(*types.Struct)
	if !isStruct {
		return Fact{}, false
	}
	var fields []string
	for i := range st.NumFields() {
		field := st.Field(i)
		if !field.Embedded() && field.Name() != "_" && conditionable(field.Type()) {
			fields = append(fields, field.Name())
		}
	}
	if len(fields) == 0 {
		return Fact{}, false
	}
	attrs := map[string]string{
		attrName:   obj.Pkg().Name() + "." + obj.Name(),
		attrFields: strings.Join(fields, ","),
	}
	if tags := datastoreNames(st); tags != "" {
		attrs[attrTags] = tags
	}
	return Fact{
		Kind: kindStruct, ID: id, Attrs: attrs,
		Source: at, Origin: origin, Certainty: certain,
	}, true
}

// datastoreNames lists the fields of st with a datastore tag as
// "Field:name", comma-separated in field order; a field the tag skips
// ("-") is left out.
func datastoreNames(st *types.Struct) string {
	var names []string
	for i := range st.NumFields() {
		name, _, _ := strings.Cut(reflect.StructTag(st.Tag(i)).Get("datastore"), ",")
		if name != "" && name != "-" {
			names = append(names, st.Field(i).Name()+":"+name)
		}
	}
	return strings.Join(names, ",")
}

// conditionable reports whether a field of type t holds a value a
// condition compares (a boolean, a number, a string, an enumeration over
// one, a time) or tests for presence (a pointer to one of those). A
// slice, map, struct or function is not one value.
func conditionable(t types.Type) bool {
	if p, isPointer := types.Unalias(t).(*types.Pointer); isPointer {
		t = p.Elem()
	}
	if named, isNamed := types.Unalias(t).(*types.Named); isNamed {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Time" {
			return true
		}
	}
	basic, isBasic := t.Underlying().(*types.Basic)
	return isBasic && basic.Info()&(types.IsBoolean|types.IsNumeric|types.IsString) != 0
}
