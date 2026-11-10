package locale_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// prose matches text a reader reads: two lowercase words in a row. Markup
// and format tokens ("```mermaid\ngraph TD", `## Contents`) have none.
var prose = regexp.MustCompile(`\p{Ll}{3,}[ ,:;'’]+\p{Ll}{2,}`)

// TestOutputStringsInLocale enforces the AGENTS.md rule that every fixed
// string of the spec output lives in this package, FR and EN side by side:
// the writers of the output (render, questions, the domain-model files,
// the access views) hold no prose literal of their own, so --lang reaches every line.
//
// Not checked: error and log messages (for the operator, not the spec),
// struct tags, and llms.txt (render/llms.go: an index for agents, kept in
// one language by decision). The exports (quire, kiro) write the text
// their pinned formats require and are not writers of the spec.
func TestOutputStringsInLocale(t *testing.T) {
	exempt := map[string]bool{"llms.go": true}
	var found []string
	for _, dir := range []string{"../render", "../questions", "../domainmodel", "../policyview"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") || exempt[filepath.Base(path)] {
				continue
			}
			found = append(found, proseLiterals(t, path)...)
		}
	}
	for _, f := range found {
		t.Errorf("%s: a fixed output string outside internal/locale (AGENTS.md); "+
			"add it to locale.Text with tr(fr, en) and write text.<Field>", f)
	}
}

// proseLiterals lists path's string literals that read as prose, as
// "file:line: text", outside error and log messages and struct tags.
func proseLiterals(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var found []string
	tags := map[*ast.BasicLit]bool{} // a struct tag names keys, it is not text
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			if x.Tag != nil {
				tags[x.Tag] = true
			}
		case *ast.CallExpr:
			return !isDiagnostic(x)
		case *ast.BasicLit:
			if x.Kind != token.STRING || tags[x] {
				return true
			}
			text, unquoteErr := strconv.Unquote(x.Value)
			if unquoteErr == nil && prose.MatchString(text) {
				found = append(found, fset.Position(x.Pos()).String()+": "+strconv.Quote(text))
			}
		}
		return true
	})
	return found
}

// isDiagnostic reports whether call builds an error or a log record: its
// text is for whoever runs codetospec, in English like the code.
func isDiagnostic(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if pkg, isIdent := sel.X.(*ast.Ident); isIdent {
		switch pkg.Name + "." + sel.Sel.Name {
		case "fmt.Errorf", "errors.New":
			return true
		}
	}
	switch sel.Sel.Name {
	case "Debug", "Info", "Warn", "Error",
		"DebugContext", "InfoContext", "WarnContext", "ErrorContext":
		return true
	}
	return false
}
