// Package sitter wraps tree-sitter behind a language-neutral API: a grammar
// registry, per-language .scm queries (definitions, imports, modules) and
// AST-based chunking. Adding a language means adding a grammar entry and a
// query file; the rest of the program never mentions a specific language.
package sitter

import (
	"cmp"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

//go:embed queries/*.scm
var queryFS embed.FS

// Definition is a named code definition located by the language query.
type Definition struct {
	Kind       string // "class", "function", "method", "interface", ...
	Name       string
	Visibility string // empty when the language/query does not provide it
	StartLine  int    // 1-based
	EndLine    int    // 1-based inclusive
}

// Import is one dependency statement located by the language query.
type Import struct {
	Target    string
	StartLine int
	EndLine   int
}

// Module is one namespace/package unit declared in a file.
type Module struct {
	Name      string
	StartLine int
	EndLine   int
}

// FileInfo is everything the universal layer extracts from one file.
type FileInfo struct {
	Language    string
	Definitions []Definition
	Imports     []Import
	Modules     []Module
}

// grammar couples a tree-sitter language constructor with its query file.
type grammar struct {
	name      string
	queryFile string
	language  func() *tree_sitter.Language

	once  sync.Once
	lang  *tree_sitter.Language
	query *tree_sitter.Query
	err   error
}

// Registered languages.
const (
	langPHP        = "php"
	langGo         = "go"
	langJavaScript = "javascript"
	langTypeScript = "typescript"
	langTSX        = "tsx"
	langRust       = "rust"
)

// newGrammars returns one uncompiled grammar per registered language.
func newGrammars() map[string]*grammar {
	return map[string]*grammar{
		langPHP: {
			name:      langPHP,
			queryFile: "queries/php.scm",
			language: func() *tree_sitter.Language {
				return tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP())
			},
		},
		langGo: {name: langGo, queryFile: "queries/go.scm", language: func() *tree_sitter.Language {
			return tree_sitter.NewLanguage(tree_sitter_go.Language())
		}},
		langJavaScript: {
			name: langJavaScript, queryFile: "queries/javascript.scm",
			language: func() *tree_sitter.Language {
				return tree_sitter.NewLanguage(tree_sitter_javascript.Language())
			},
		},
		langTypeScript: {
			name: langTypeScript, queryFile: "queries/typescript.scm",
			language: func() *tree_sitter.Language {
				return tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
			},
		},
		langTSX: {
			name:      langTSX,
			queryFile: "queries/tsx.scm",
			language: func() *tree_sitter.Language {
				return tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX())
			},
		},
		langRust: {
			name:      langRust,
			queryFile: "queries/rust.scm",
			language: func() *tree_sitter.Language {
				return tree_sitter.NewLanguage(tree_sitter_rust.Language())
			},
		},
	}
}

// LanguageForPath returns the registered language for a file path, or ""
// when no grammar matches its extension.
func LanguageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".php":
		return langPHP
	case ".go":
		return langGo
	case ".js", ".jsx", ".mjs", ".cjs":
		return langJavaScript
	case ".ts":
		return langTypeScript
	case ".tsx":
		return langTSX
	case ".rs":
		return langRust
	default:
		return ""
	}
}

// Parser parses source files with the registered grammars, compiling each
// grammar's query once, on first use. It is safe for concurrent use; one
// run shares one Parser.
type Parser struct {
	grammars map[string]*grammar
}

// NewParser returns a Parser for every registered language.
func NewParser() *Parser {
	return &Parser{grammars: newGrammars()}
}

func (g *grammar) compile() (*tree_sitter.Language, *tree_sitter.Query, error) {
	g.once.Do(func() {
		src, err := queryFS.ReadFile(g.queryFile)
		if err != nil {
			g.err = fmt.Errorf("read query %s: %w", g.queryFile, err)
			return
		}
		g.lang = g.language()
		query, queryErr := tree_sitter.NewQuery(g.lang, string(src))
		if queryErr != nil {
			g.err = fmt.Errorf("compile query %s: %w", g.queryFile, *queryErr)
			return
		}
		g.query = query
	})
	return g.lang, g.query, g.err
}

// Parse extracts definitions, imports and modules from one source file.
func (p *Parser) Parse(language string, content []byte) (*FileInfo, error) {
	g, ok := p.grammars[language]
	if !ok {
		return nil, fmt.Errorf("no grammar registered for language %q", language)
	}
	lang, query, err := g.compile()
	if err != nil {
		return nil, err
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()
	if setErr := parser.SetLanguage(lang); setErr != nil {
		return nil, fmt.Errorf("set language %s: %w", language, setErr)
	}
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("tree-sitter returned no tree for language %s", language)
	}
	defer tree.Close()

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(query, tree.RootNode(), content)
	names := query.CaptureNames()

	info := &FileInfo{Language: language}
	for m := matches.Next(); m != nil; m = matches.Next() {
		info.add(capturesOf(m, names), content)
	}
	dedupInfo(info)
	return info, nil
}

// captures are the nodes one query match names.
type captures struct {
	def, name, visibility *tree_sitter.Node
	module, moduleName    *tree_sitter.Node
	imp, impTarget        *tree_sitter.Node
	defKind               string // the "def.<kind>" capture's kind
}

// capturesOf sorts a match's captures by the name the query gives them.
func capturesOf(m *tree_sitter.QueryMatch, names []string) captures {
	var c captures
	for i := range m.Captures {
		node := &m.Captures[i].Node
		switch captureName := names[m.Captures[i].Index]; {
		case strings.HasPrefix(captureName, "def."):
			c.def, c.defKind = node, strings.TrimPrefix(captureName, "def.")
		case captureName == "name":
			c.name = node
		case captureName == "visibility":
			c.visibility = node
		case captureName == "module":
			c.module = node
		case captureName == "module.name":
			c.moduleName = node
		case captureName == "import":
			c.imp = node
		case captureName == "import.target":
			c.impTarget = node
		}
	}
	return c
}

// add records what one match describes: a definition, a module or an
// import.
func (info *FileInfo) add(c captures, content []byte) {
	switch {
	case c.def != nil && c.name != nil:
		d := Definition{
			Kind:      c.defKind,
			Name:      c.name.Utf8Text(content),
			StartLine: startLine(c.def),
			EndLine:   endLine(c.def),
		}
		if c.visibility != nil {
			d.Visibility = c.visibility.Utf8Text(content)
		}
		info.Definitions = append(info.Definitions, d)
	case c.module != nil && c.moduleName != nil:
		info.Modules = append(info.Modules, Module{
			Name:      c.moduleName.Utf8Text(content),
			StartLine: startLine(c.module),
			EndLine:   endLine(c.module),
		})
	case c.imp != nil || c.impTarget != nil:
		span := cmp.Or(c.imp, c.impTarget)
		target := span.Utf8Text(content)
		if c.impTarget != nil {
			target = c.impTarget.Utf8Text(content)
		}
		info.Imports = append(info.Imports, Import{
			Target:    cleanImportTarget(target),
			StartLine: startLine(span),
			EndLine:   endLine(span),
		})
	}
}

func startLine(n *tree_sitter.Node) int {
	return int(n.StartPosition().Row) + 1
}

// endLine converts an exclusive end position into a 1-based inclusive line.
func endLine(n *tree_sitter.Node) int {
	pos := n.EndPosition()
	if pos.Column == 0 && pos.Row > 0 {
		return int(pos.Row)
	}
	return int(pos.Row) + 1
}

// cleanImportTarget strips quotes, trailing separators and language noise
// (aliases after " as ") from an import target.
func cleanImportTarget(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " as "); i > 0 {
		s = s[:i]
	}
	s = strings.Trim(s, "\"'`;")
	return strings.TrimSpace(s)
}

// dedupInfo removes duplicate query matches and sorts everything stably.
func dedupInfo(info *FileInfo) {
	seenDefs := make(map[Definition]bool)
	defs := info.Definitions[:0]
	for _, d := range info.Definitions {
		key := d
		key.Visibility = ""
		if !seenDefs[key] {
			seenDefs[key] = true
			defs = append(defs, d)
		}
	}
	info.Definitions = defs
	sort.SliceStable(info.Definitions, func(i, j int) bool {
		a, b := info.Definitions[i], info.Definitions[j]
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.Name < b.Name
	})

	seenImports := make(map[Import]bool)
	imports := info.Imports[:0]
	for _, imp := range info.Imports {
		if !seenImports[imp] {
			seenImports[imp] = true
			imports = append(imports, imp)
		}
	}
	info.Imports = imports
	sort.SliceStable(info.Imports, func(i, j int) bool {
		a, b := info.Imports[i], info.Imports[j]
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.Target < b.Target
	})

	seenModules := make(map[Module]bool)
	modules := info.Modules[:0]
	for _, m := range info.Modules {
		if !seenModules[m] {
			seenModules[m] = true
			modules = append(modules, m)
		}
	}
	info.Modules = modules
	sort.SliceStable(info.Modules, func(i, j int) bool {
		return info.Modules[i].StartLine < info.Modules[j].StartLine
	})
}
