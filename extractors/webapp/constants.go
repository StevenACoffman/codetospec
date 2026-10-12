package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// httpStatusFile declares net/http's Status* constants, relative to GOROOT.
const httpStatusFile = "src/net/http/status.go"

// constant is a named numeric value as code refers to it.
type constant struct {
	name  string // "http.StatusBadRequest", "models.MaxNonces" or bare "maxNonces"
	value string
	src   Ref
}

// constantFacts binds the numeric constants a requirement may state to the
// names the code uses for them: every package-level constant of the service
// with a literal value (qualified "pkg.Name"; bare "Name" too when no other
// package declares it), and net/http's status codes read from goroot.
func constantFacts(goroot fs.FS, sources []source) ([]Fact, []string) {
	var (
		warnings []string
		consts   []constant
	)
	bare := make(map[string][]constant)
	for i := range sources {
		for _, c := range literalConstants(&sources[i]) {
			_, short, _ := strings.Cut(c.name, ".")
			bare[short] = append(bare[short], c)
			consts = append(consts, c)
		}
	}
	for short, same := range bare {
		if len(same) == 1 {
			consts = append(consts, constant{name: short, value: same[0].value, src: same[0].src})
		}
	}
	if status, err := parseGo(goroot, httpStatusFile); err != nil {
		warnings = append(warnings, fmt.Sprintf("constants: no net/http status codes: %v", err))
	} else {
		for _, c := range literalConstants(&status) {
			c.src = Ref{} // outside the service
			consts = append(consts, c)
		}
	}

	sort.Slice(consts, func(i, j int) bool { return consts[i].name < consts[j].name })
	facts := make([]Fact, 0, len(consts))
	for _, c := range consts {
		facts = append(facts, Fact{
			Kind:      kindConstant,
			ID:        "constant.webapp." + c.name,
			Attrs:     map[string]string{attrName: c.name, attrValue: c.value},
			Source:    c.src,
			Origin:    origin,
			Certainty: certain,
		})
	}
	return facts, warnings
}

// literalConstants returns the package-level constants of one file whose
// value is a single numeric literal, qualified by the file's package name.
func literalConstants(s *source) []constant {
	var consts []constant
	for _, decl := range s.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue || len(vs.Names) != len(vs.Values) {
				continue // iota blocks and implicit repetition carry no literal
			}
			for i, ident := range vs.Names {
				value, numeric := numericLiteral(vs.Values[i])
				if !numeric || ident.Name == "_" {
					continue
				}
				line := strconv.Itoa(s.fset.Position(ident.Pos()).Line)
				consts = append(consts, constant{
					name:  s.file.Name.Name + "." + ident.Name,
					value: value,
					src:   Ref{Path: s.name, Lines: line + "-" + line},
				})
			}
		}
	}
	return consts
}

// numericLiteral returns e's value when e is an integer or float literal,
// possibly negated, with digit separators removed.
func numericLiteral(e ast.Expr) (string, bool) {
	sign := ""
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		sign, e = "-", u.X
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || (lit.Kind != token.INT && lit.Kind != token.FLOAT) {
		return "", false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(lit.Value, "_", ""), 64)
	if err != nil {
		// Hex, octal and binary integers: let strconv read the base.
		n, intErr := strconv.ParseInt(strings.ReplaceAll(lit.Value, "_", ""), 0, 64)
		if intErr != nil {
			return "", false
		}
		v = float64(n)
	}
	return sign + strconv.FormatFloat(v, 'f', -1, 64), true
}
