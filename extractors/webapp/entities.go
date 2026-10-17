package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// registration is one datastore.RegisterKind("Kind", new(T)) call.
type registration struct {
	kind, typeName, dir string
}

// structDecl is a struct type declaration with its datastore columns.
type structDecl struct {
	file        string
	start, end  int
	columns     []string // "name:type"
	description string   // its doc comment on one line; "" when none
}

// entityFacts turns every Datastore kind the service registers into a
// "table" fact: name = the kind, columns = the registered struct's fields as
// Datastore stores them, description = the struct's doc comment, source =
// the struct declaration.
func entityFacts(sources []source) ([]Fact, []string) {
	var (
		warnings []string
		regs     []registration
	)
	structs := make(map[string]structDecl) // dir + "." + type name
	for i := range sources {
		s := &sources[i]
		dir := path.Dir(s.name)
		regs = append(regs, registrations(s.file, dir)...)
		for typeName, decl := range structDecls(s.fset, s.file, s.name) {
			structs[dir+"."+typeName] = decl
		}
	}

	facts := make([]Fact, 0, len(regs))
	for _, r := range regs {
		decl, ok := structs[r.dir+"."+r.typeName]
		if !ok {
			warnings = append(
				warnings,
				fmt.Sprintf(
					"entities: kind %s: struct %s not found in %s",
					r.kind,
					r.typeName,
					r.dir,
				),
			)
			continue
		}
		attrs := map[string]string{
			attrName:    r.kind,
			attrColumns: strings.Join(decl.columns, ", "),
		}
		if decl.description != "" {
			attrs[attrDescription] = decl.description
		}
		facts = append(facts, Fact{
			Kind:      kindTable,
			ID:        "table.webapp." + r.kind,
			Attrs:     attrs,
			Source:    Ref{Path: decl.file, Lines: lines(decl.start, decl.end)},
			Origin:    origin,
			Certainty: certain,
		})
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	return facts, warnings
}

// registrations finds datastore.RegisterKind("Kind", new(T)) calls.
func registrations(file *ast.File, dir string) []registration {
	var regs []registration
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 || !isSelector(call.Fun, "datastore", "RegisterKind") {
			return true
		}
		kindLit, isLit := call.Args[0].(*ast.BasicLit)
		newCall, isCall := call.Args[1].(*ast.CallExpr)
		if !isLit || kindLit.Kind != token.STRING || !isCall || len(newCall.Args) != 1 {
			return true
		}
		if fun, isIdent := newCall.Fun.(*ast.Ident); !isIdent || fun.Name != "new" {
			return true
		}
		typeIdent, isIdent := newCall.Args[0].(*ast.Ident)
		kind, err := strconv.Unquote(kindLit.Value)
		if !isIdent || err != nil {
			return true
		}
		regs = append(regs, registration{kind: kind, typeName: typeIdent.Name, dir: dir})
		return true
	})
	return regs
}

// structDecls returns the struct types declared in file, by name.
func structDecls(fset *token.FileSet, file *ast.File, name string) map[string]structDecl {
	decls := make(map[string]structDecl)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, isType := spec.(*ast.TypeSpec)
			if !isType {
				continue
			}
			st, isStruct := ts.Type.(*ast.StructType)
			if !isStruct {
				continue
			}
			decls[ts.Name.Name] = structDecl{
				file:        name,
				start:       fset.Position(ts.Pos()).Line,
				end:         fset.Position(ts.End()).Line,
				columns:     datastoreColumns(fset, st),
				description: typeDoc(gen, ts),
			}
		}
	}
	return decls
}

// typeDoc is the doc comment of a type, on one line: the spec's own, or
// its declaration's when the declaration holds this type only.
func typeDoc(gen *ast.GenDecl, ts *ast.TypeSpec) string {
	doc := ts.Doc
	if doc == nil && len(gen.Specs) == 1 {
		doc = gen.Doc
	}
	if doc == nil {
		return ""
	}
	return strings.Join(strings.Fields(doc.Text()), " ")
}

// datastoreColumns lists the fields Datastore stores: the `datastore:"name"`
// tag name, else the exported field name; "-" and unexported fields are
// skipped, and so are embedded fields (their columns live elsewhere).
func datastoreColumns(fset *token.FileSet, st *ast.StructType) []string {
	var columns []string
	for _, field := range st.Fields.List {
		tagName := ""
		if field.Tag != nil {
			if tag, err := strconv.Unquote(field.Tag.Value); err == nil {
				tagName, _, _ = strings.Cut(reflect.StructTag(tag).Get("datastore"), ",")
			}
		}
		if tagName == "-" {
			continue
		}
		var typ bytes.Buffer
		if err := printer.Fprint(&typ, fset, field.Type); err != nil {
			continue
		}
		for _, ident := range field.Names {
			if !ident.IsExported() {
				continue
			}
			column := ident.Name
			if tagName != "" {
				column = tagName
			}
			columns = append(columns, column+":"+typ.String())
		}
	}
	return columns
}

// isSelector reports whether e is pkg.name.
func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg
}
