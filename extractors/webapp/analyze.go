package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// resolversDir holds a webapp service's gqlgen resolvers.
const resolversDir = "resolvers"

// fallbackDomain is the domain of a resolver method whose field no schema
// file declares: the directory domain codetospec would assign anyway.
const fallbackDomain = "resolvers"

// Analyze derives rule and domain facts from a webapp service rooted at
// fsys, writing requirements in lang ("fr" or "en").
//
// Requires: fsys is a service root (holding resolvers/ and gqlgen.yml).
// Ensures:  facts are sorted (see sortFacts); files that do not parse and resolver
// methods with no schema field are reported in warnings, not errors; err is
// set only when fsys itself cannot be read.
func Analyze(fsys fs.FS, lang string) ([]Fact, []string, error) {
	schema, warnings, err := loadSchema(fsys)
	if err != nil {
		return nil, warnings, err
	}
	names, err := fs.Glob(fsys, resolversDir+"/*.go")
	if err != nil {
		return nil, warnings, fmt.Errorf("list resolvers: %w", err)
	}
	w := wordingFor(lang)
	fset := token.NewFileSet()
	var facts []Fact
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			return nil, warnings, fmt.Errorf("read %s: %w", name, readErr)
		}
		file, parseErr := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if parseErr != nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: %v", name, parseErr))
			continue
		}
		for _, m := range resolverMethods(fset, file) {
			field, schemaFile := schema.lookup(m.typeName, m.method)
			domain := fallbackDomain
			if schemaFile != "" {
				domain = domainOf(schemaFile)
				facts = append(facts, domainFact(name, &m, domain))
			} else {
				warnings = append(
					warnings,
					fmt.Sprintf("%s: %s.%s matches no schema field", name, m.typeName, m.method),
				)
			}
			facts = append(facts, ruleFacts(fset, &w, name, &m, m.typeName+"."+field, domain)...)
		}
	}
	sortFacts(facts)
	return facts, warnings, nil
}

// sortFacts orders facts by kind, file and start line (numerically, so line
// 6 precedes line 58), then ID — deterministic and readable.
func sortFacts(facts []Fact) {
	start := func(f *Fact) int {
		first, _, _ := strings.Cut(f.Source.Lines, "-")
		n, _ := strconv.Atoi(first)
		return n
	}
	sort.SliceStable(facts, func(i, j int) bool {
		a, b := &facts[i], &facts[j]
		switch {
		case a.Kind != b.Kind:
			return a.Kind < b.Kind
		case a.Source.Path != b.Source.Path:
			return a.Source.Path < b.Source.Path
		case start(a) != start(b):
			return start(a) < start(b)
		}
		return a.ID < b.ID
	})
}

// domainOf names a domain after its schema file: auth_emails.graphql →
// auth-emails.
func domainOf(schemaFile string) string {
	return slugify(strings.TrimSuffix(path.Base(schemaFile), path.Ext(schemaFile)))
}

func domainFact(file string, m *resolverMethod, domain string) Fact {
	return Fact{
		Kind:      kindDomain,
		ID:        fmt.Sprintf("domain.webapp.%s:%d", file, m.start),
		Attrs:     map[string]string{attrDomain: domain},
		Source:    Ref{Path: file, Lines: lines(m.start, m.end)},
		Origin:    origin,
		Certainty: certain,
	}
}

// ruleFacts turns each guard of m into a rule fact for operation.
func ruleFacts(
	fset *token.FileSet,
	w *wording,
	file string,
	m *resolverMethod,
	operation, domain string,
) []Fact {
	facts := make([]Fact, 0, len(m.guards))
	for i := range m.guards {
		g := &m.guards[i]
		slug := slugify("authz-" + operation)
		title := fmt.Sprintf(w.titleFmt, operation)
		if i > 0 {
			slug += "-" + strconv.Itoa(i+1)
			title += " (" + strconv.Itoa(i+1) + ")"
		}
		ears := "unwanted"
		if g.open {
			ears = "ubiquitous"
		}
		facts = append(facts, Fact{
			Kind: kindRule,
			ID:   fmt.Sprintf("rule.webapp.authz.%s:%d", file, g.start),
			Attrs: map[string]string{
				attrSlug:        slug,
				attrTitle:       title,
				attrRequirement: w.requirement(fset, g, operation),
				attrEARS:        ears,
				attrNature:      "business",
				attrDomain:      domain,
			},
			Source:    Ref{Path: file, Lines: lines(g.start, g.end)},
			Origin:    origin,
			Certainty: certain,
		})
	}
	return facts
}

func lines(start, end int) string {
	return strconv.Itoa(start) + "-" + strconv.Itoa(end)
}

// slugify lowercases s and replaces every run of non-alphanumerics with "-",
// matching codetospec's slug contract.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
