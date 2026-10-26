// Command scip2facts converts a SCIP code-intelligence index into codetospec
// facts (protocol facts v1). It emits `symbol` facts with the EXACT
// definition ranges an indexer resolved — far more precise than tree-sitter,
// which is the point: precise symbol locations let the map/verify phases stop
// citing empty lines or the wrong span.
//
// SCIP indexes are produced by language indexers (scip-go, scip-typescript,
// scip-clang, …). This tool reads their output; it never runs them.
//
// Usage: scip2facts --index index.scip [--root <src>]
//
// The tool speaks the extractor protocol, so codetospec consumes it via
// --facts or an extractor entry, with zero changes to the main binary.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

func main() {
	indexPath := flag.String("index", "", "path to the SCIP index (index.scip) (required)")
	root := flag.String("root", "", "optional source root prefix to strip from document paths")
	flag.Parse()

	if *indexPath == "" {
		fmt.Fprintln(os.Stderr, "scip2facts: --index is required")
		os.Exit(1)
	}
	data, err := os.ReadFile(*indexPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scip2facts: read index: %v\n", err)
		os.Exit(1)
	}
	var index scip.Index
	if err = proto.Unmarshal(data, &index); err != nil {
		fmt.Fprintf(os.Stderr, "scip2facts: parse SCIP index: %v\n", err)
		os.Exit(1)
	}

	facts := convert(&index, *root)
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	if err = json.NewEncoder(os.Stdout).
		Encode(Envelope{Schema: schemaID, Facts: facts}); err != nil {
		fmt.Fprintf(os.Stderr, "scip2facts: encode: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "scip2facts: %d symbol fact(s)\n", len(facts))
}

// convert turns definition occurrences into precise symbol facts.
func convert(index *scip.Index, root string) []Fact {
	var facts []Fact
	for _, doc := range index.GetDocuments() {
		path := normalizePath(doc.GetRelativePath(), root)
		if !isLocalPath(path) {
			continue // stdlib, dependencies, generated build artifacts
		}
		language := strings.ToLower(doc.GetLanguage())

		info := make(map[string]*scip.SymbolInformation, len(doc.GetSymbols()))
		for _, s := range doc.GetSymbols() {
			info[s.GetSymbol()] = s
		}

		for _, occ := range doc.GetOccurrences() {
			if occ.GetSymbolRoles()&int32(scip.SymbolRole_Definition) == 0 {
				continue
			}
			if !citable(occ.GetSymbol()) {
				continue
			}
			start, end, ok := definitionLines(occ)
			if !ok {
				continue
			}
			f := symbolFact(occ.GetSymbol(), info[occ.GetSymbol()], path, language, start, end)
			if f != nil {
				facts = append(facts, *f)
			}
		}
	}
	return dedup(facts)
}

// citable reports whether a symbol is a definition a business rule can be
// about: a function, method, type or package-level term. Locals,
// parameters, type parameters, packages and fields (terms of a type) are
// dropped: indexers emit tens of them per file, and listing them in every
// map prompt would cost far more than they help.
func citable(symbol string) bool {
	if scip.IsLocalSymbol(symbol) {
		return false
	}
	parsed, err := scip.ParseSymbol(symbol)
	if err != nil || len(parsed.GetDescriptors()) == 0 {
		return false
	}
	last := len(parsed.GetDescriptors()) - 1
	switch parsed.GetDescriptors()[last].GetSuffix() {
	case scip.Descriptor_Method, scip.Descriptor_Type, scip.Descriptor_Macro:
		return true
	case scip.Descriptor_Term:
		for _, d := range parsed.GetDescriptors()[:last] {
			if d.GetSuffix() == scip.Descriptor_Type {
				return false // a field
			}
		}
		return true
	case scip.Descriptor_UnspecifiedSuffix, scip.Descriptor_Namespace, // Package is an alias
		scip.Descriptor_TypeParameter, scip.Descriptor_Parameter,
		scip.Descriptor_Meta, scip.Descriptor_Local:
		return false
	}
	return false
}

// definitionLines returns the 1-based inclusive line span of a definition,
// preferring the enclosing (body) range over the identifier range. It reads
// the raw []int32 range fields, which is what indexers actually emit.
func definitionLines(occ *scip.Occurrence) (int, int, bool) {
	raw := occ.GetEnclosingRange() //nolint:staticcheck // indexers emit the raw range field
	if len(raw) == 0 {
		raw = occ.GetRange() //nolint:staticcheck // indexers emit the raw range field
	}
	r, err := scip.NewRange(raw)
	if err != nil {
		return 0, 0, false
	}
	return int(r.Start.Line) + 1, int(r.End.Line) + 1, true
}

// symbolFact builds a symbol fact from a definition, resolving the name,
// kind, namespace and signature from SCIP metadata when available.
func symbolFact(
	symbol string,
	si *scip.SymbolInformation,
	path, language string,
	start, end int,
) *Fact {
	name := ""
	kind := ""
	namespace := ""
	signature := ""
	if si != nil {
		name = si.GetDisplayName()
		kind = symbolKind(si.GetKind())
		if sig := si.GetSignatureDocumentation(); sig != nil {
			signature = strings.TrimSpace(sig.GetText())
		}
	}
	if parsed, err := scip.ParseSymbol(symbol); err == nil {
		if name == "" && len(parsed.GetDescriptors()) > 0 {
			name = parsed.GetDescriptors()[len(parsed.GetDescriptors())-1].GetName()
		}
		namespace = qualifier(parsed)
		if language == "" && parsed.GetPackage() != nil {
			language = ""
		}
	}
	if name == "" {
		return nil // an anonymous or unresolvable symbol is not useful
	}

	attrs := map[string]string{"name": name, "precise": "true"}
	if kind != "" {
		attrs["kind"] = kind
	}
	if language != "" {
		attrs["language"] = language
	}
	if namespace != "" {
		attrs["namespace"] = namespace
	}
	if signature != "" {
		attrs["signature"] = truncate(signature, signatureMaxLen)
	}
	return &Fact{
		Kind:      "symbol",
		ID:        fmt.Sprintf("symbol.%s#%s:%d", path, name, start),
		Attrs:     attrs,
		Source:    Ref{Path: path, Lines: fmt.Sprintf("%d-%d", start, end)},
		Origin:    origin,
		Certainty: certain,
	}
}

// signatureMaxLen caps a signature carried in a fact.
const signatureMaxLen = 400

// qualifiedDescriptors is the fewest descriptors a qualified symbol has:
// its own and at least one enclosing.
const qualifiedDescriptors = 2

// qualifier joins the enclosing descriptors (namespace/type path) of a
// symbol, excluding its own final descriptor.
func qualifier(sym *scip.Symbol) string {
	if len(sym.GetDescriptors()) < qualifiedDescriptors {
		return ""
	}
	parts := make([]string, 0, len(sym.GetDescriptors())-1)
	for _, d := range sym.GetDescriptors()[:len(sym.GetDescriptors())-1] {
		if d.GetName() != "" {
			parts = append(parts, d.GetName())
		}
	}
	return strings.Join(parts, ".")
}

// symbolKind maps the SCIP kinds codetospec distinguishes; every other kind
// (of the dozens SCIP defines) has none.
func symbolKind(k scip.SymbolInformation_Kind) string {
	kinds := []struct {
		kind scip.SymbolInformation_Kind
		name string
	}{
		{scip.SymbolInformation_Class, "class"},
		{scip.SymbolInformation_Struct, "class"},
		{scip.SymbolInformation_Interface, "interface"},
		{scip.SymbolInformation_Method, "method"},
		{scip.SymbolInformation_Function, "function"},
		{scip.SymbolInformation_Enum, "enum"},
		{scip.SymbolInformation_Trait, "trait"},
	}
	for _, entry := range kinds {
		if entry.kind == k {
			return entry.name
		}
	}
	return ""
}

func normalizePath(path, root string) string {
	path = strings.TrimPrefix(path, "./")
	if root != "" {
		path = strings.TrimPrefix(path, strings.TrimSuffix(root, "/")+"/")
	}
	return path
}

// isLocalPath keeps only paths that live inside the analyzed repository —
// not stdlib, dependencies or generated build artifacts an indexer may pull
// in from outside the tree.
func isLocalPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "..") || strings.HasPrefix(path, "/") {
		return false
	}
	return !strings.Contains(path, "go-build") && !strings.Contains(path, "/pkg/mod/")
}

func dedup(facts []Fact) []Fact {
	seen := make(map[string]bool, len(facts))
	out := facts[:0]
	for _, f := range facts {
		if !seen[f.ID] {
			seen[f.ID] = true
			out = append(out, f)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
