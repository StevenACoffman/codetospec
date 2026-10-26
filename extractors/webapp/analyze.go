package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// resolversDir holds a webapp service's gqlgen resolvers.
const resolversDir = "resolvers"

// fallbackDomain is the domain of a resolver method whose field no schema
// file declares: the directory domain codetospec would assign anyway.
const fallbackDomain = "resolvers"

// Inputs are the file trees and settings Analyze reads.
type Inputs struct {
	Service fs.FS // the service root, holding resolvers/ and gqlgen.yml
	// Webapp is the webapp checkout the service lives in: the checkers its
	// shared packages (pkg/) mark are known by what they are; nil skips it.
	Webapp fs.FS
	// Supergraph holds composed_schema.graphql, the federated schema that
	// says which service owns each operation; nil skips dependencies.
	Supergraph   fs.FS
	ServiceGraph string // this service's subgraph name, e.g. "USERS"
	Lang         string // requirement language, "fr" or "en"
	// AccessPkgs names the service's own access packages (permissions):
	// their exported functions returning bool first are access checks.
	AccessPkgs []string
}

// Analyze derives facts from a webapp service.
//
// Requires: in.Service is a service root.
// Ensures:  facts are sorted (see sortFacts); files that do not parse and resolver
// methods with no schema field are reported in warnings, not errors; err is
// set only when the service itself cannot be read.
func Analyze(in *Inputs) ([]Fact, []string, error) {
	fsys := in.Service
	schema, warnings, err := loadSchema(fsys)
	if err != nil {
		return nil, warnings, err
	}
	names, err := goSources(fsys)
	if err != nil {
		return nil, warnings, err
	}
	// Parse once; every scan reads the same trees.
	sources, parseWarnings := parseSources(fsys, names)
	warnings = append(warnings, parseWarnings...)

	w := wordingFor(in.Lang)
	shared, pkgWarnings := importedPackages(in.Webapp, sources)
	warnings = append(warnings, pkgWarnings...)
	known, sharedWarnings := knownChecks(sources, shared.sources, in)
	found := policies(sources, known)
	reach := reached(sources, shared)
	resolverFacts, resolverWarnings := resolverScan(sources, schema, known, &w)
	entFacts, entWarnings := entityFacts(sources)
	depFacts, depWarnings := dependencyFacts(in, sources)

	facts := slices.Concat(
		resolverFacts,
		entryScan(sources, shared.sources, known, &w),
		policyFacts(found, &w, domainAt(resolverFacts)),
		tableFacts(sources, found, &w),
		sharedFacts(shared, reach, known, &w),
		contractFacts(sources, shared, reach, &w),
		servedFacts(sources, &w),
		reachFacts(shared, reach),
		entFacts,
		depFacts,
		roleFacts(sources, &w),
	)
	warnings = slices.Concat(
		warnings, sharedWarnings, resolverWarnings, entWarnings, depWarnings)
	sortFacts(facts)
	return facts, warnings, nil
}

// domainAt returns the domain of a line: that of the smallest domain fact
// covering it (a resolver helper's), else its directory's (see dirDomain).
func domainAt(facts []Fact) func(file string, line int) string {
	return func(file string, line int) string {
		best, span := dirDomain(file), -1
		for _, f := range facts {
			if f.Kind != kindDomain || f.Source.Path != file {
				continue
			}
			var start, end int
			if _, err := fmt.Sscanf(f.Source.Lines, "%d-%d", &start, &end); err != nil {
				continue
			}
			if start <= line && line <= end && (span < 0 || end-start < span) {
				best, span = f.Attrs[attrDomain], end-start
			}
		}
		return best
	}
}

// knownChecks returns the checks the service's code is scanned against:
// the checkers it and webapp's shared packages mark (the service wins on a
// name), the authenticators of both (see authenticators; shared are the
// pkg/ packages it imports), those deriving their denial from them, and
// the access packages' predicates. Helper predicates depend on the files scanned: callers add
// them.
func knownChecks(sources, shared []source, in *Inputs) (checks, []string) {
	marked := markedCheckers(sources)
	sharedMarked, warnings := sharedCheckers(in.Webapp)
	authn := make(map[string]checker)
	for _, f := range authenticators(sources) {
		authn[f.name] = checker{errDenies: true} // called unqualified in its package
		authn[f.pkg+"."+f.name] = checker{errDenies: true}
	}
	for _, f := range authenticators(shared) {
		authn[f.pkg+"."+f.name] = checker{errDenies: true}
	}
	for _, more := range []map[string]checker{sharedMarked, authn} {
		for key, c := range more {
			if _, own := marked[key]; !own {
				marked[key] = c
			}
		}
	}
	deriveDenials(sources, marked)
	known := checks{
		marked:      marked,
		predicates:  accessPredicates(sources, in.AccessPkgs),
		errVars:     errorVars(sources),
		credentials: credentials(sources),
	}
	maps.Copy(marked, checkBackedDenials(sources, known))
	deriveDenials(sources, marked) // delegating to what that added
	return checks{
		marked:      marked,
		predicates:  accessPredicates(sources, in.AccessPkgs),
		errVars:     errorVars(sources),
		credentials: credentials(sources),
	}, warnings
}

// resolverScan derives the rule, domain and route facts of the resolver
// methods in resolvers/.
func resolverScan(
	sources []source,
	schema schemaIndex,
	known checks,
	w *wording,
) ([]Fact, []string) {
	var resolverFiles []*source
	for i := range sources {
		if path.Dir(sources[i].name) == resolversDir {
			resolverFiles = append(resolverFiles, &sources[i])
		}
	}
	trees := make([]*ast.File, len(resolverFiles))
	for i, s := range resolverFiles {
		trees[i] = s.file
	}
	known.helpers = helperPredicates(trees, known)
	rs := resolverScanner{schema: schema, w: *w}
	types := resolverTypes(trees, schema)
	methods := make([][]resolverMethod, len(resolverFiles))
	for i, s := range resolverFiles {
		methods[i] = resolverMethods(s.fset, s.file, known, types)
	}
	methods = keepBackedDelegations(methods)
	rs.catchAll = catchAllSchemas(schema, resolverFiles, methods)
	rs.private = rs.privateOps(resolverFiles, methods)
	var (
		facts    []Fact
		warnings []string
	)
	for i, s := range resolverFiles {
		fileFacts, fileWarnings := rs.scanFile(s, methods[i])
		facts = append(facts, fileFacts...)
		warnings = append(warnings, fileWarnings...)
	}
	return facts, warnings
}

// resolverScanner holds what every resolver file is scanned against.
type resolverScanner struct {
	schema   schemaIndex
	catchAll map[string]bool // schema files too broad to name a domain
	// private holds the operation of each private resolver method
	// ("mutationResolver._helper") whose callers agree on one.
	private map[string]operation
	w       wording
}

// A schema file is a catch-all, naming no domain, when it declares the
// fields of more than catchAllShare of the service's resolver methods and
// those methods live in at least catchAllFiles resolver files: districts'
// schema.graphql declares 92% of its fields across 37 files. The file
// count keeps a small service from being split by file. gqlgen's default
// schema file is a catch-all too beside topical ones (see defaultSchema).
const (
	catchAllShare = 0.5
	catchAllFiles = 10
)

// catchAllSchemas returns the catch-all schema files; methods[i] are the
// resolver methods of resolverFiles[i].
func catchAllSchemas(
	schema schemaIndex,
	resolverFiles []*source,
	methods [][]resolverMethod,
) map[string]bool {
	perSchema := make(map[string]int)
	spread := make(map[string]map[string]bool) // schema file → resolver files
	total := 0
	for i, fileMethods := range methods {
		for j := range fileMethods {
			f := schema.lookup(fileMethods[j].typeName, fileMethods[j].method)
			if f.file == "" {
				continue
			}
			perSchema[f.file]++
			total++
			if spread[f.file] == nil {
				spread[f.file] = make(map[string]bool)
			}
			spread[f.file][resolverFiles[i].name] = true
		}
	}
	catchAll := make(map[string]bool)
	for file, n := range perSchema {
		dominant := float64(n) > catchAllShare*float64(total) && len(spread[file]) >= catchAllFiles
		if dominant || isDefaultSchema(file) && len(perSchema) > 1 {
			catchAll[file] = true
		}
	}
	return catchAll
}

// defaultSchema is gqlgen's single-file schema name. A service that also
// has topical schema files keeps it for whatever fits none of them: the
// users service's declares device auth, Clever info and terms acceptance.
const defaultSchema = "schema.graphql"

// isDefaultSchema reports whether a schema file is gqlgen's default one.
func isDefaultSchema(file string) bool {
	return path.Base(file) == defaultSchema
}

// domainFor names the domain of a resolver method in file whose field is
// declared in schemaFile: the schema file, or for a catch-all schema file
// the resolver file (resolvers/user_district_info.go → user-district-info).
func (rs *resolverScanner) domainFor(file, schemaFile string) string {
	if rs.catchAll[schemaFile] {
		return domainOf(file)
	}
	return domainOf(schemaFile)
}

// scanFile derives the facts of one resolver file's methods, plus a
// whole-file domain fact when they all share one domain.
func (rs *resolverScanner) scanFile(s *source, methods []resolverMethod) ([]Fact, []string) {
	var (
		facts    []Fact
		warnings []string
	)
	fileDomains := make(map[string]bool)
	for _, m := range methods {
		op, field := rs.operationOf(s.name, &m)
		switch {
		case m.private:
			// A helper: its rules are its callers' operation's, when they
			// agree on one; it is never a field of its own.
			if serves, ok := rs.private[m.recv+"."+m.method]; ok {
				op = serves
				fileDomains[op.domain] = true
				facts = append(facts, domainFact(s.name, m.start, m.end, op.domain))
			}
		case field.file == "":
			warnings = append(
				warnings,
				fmt.Sprintf("%s: %s.%s matches no schema field", s.name, m.typeName, m.method),
			)
		default:
			fileDomains[op.domain] = true
			facts = append(facts, rs.fieldFacts(s.name, &m, &field, &op)...)
		}
		facts = append(facts, ruleFacts(s.fset, &rs.w, s.name, &m, &op)...)
	}
	if len(fileDomains) == 1 {
		// The file's helpers serve its resolvers: give the whole file their
		// domain. Method facts still win (smaller span).
		for domain := range fileDomains {
			facts = append(facts, domainFact(s.name, 1, s.fset.Position(s.file.End()).Line, domain))
		}
	}
	return facts, warnings
}

// operationOf returns the operation a resolver method of file implements,
// and its schema field (no file when none matches: the fallback domain).
func (rs *resolverScanner) operationOf(file string, m *resolverMethod) (operation, schemaField) {
	field := rs.schema.lookup(m.typeName, m.method)
	op := operation{name: m.typeName + "." + field.name, domain: fallbackDomain}
	if field.file != "" {
		op.domain = rs.domainFor(file, field.file)
	}
	return op, field
}

// privateOps returns the operation each private resolver method serves:
// the one the methods calling it on the same receiver type agree on,
// through chains of private methods, to a fixed point. Its route is the
// callers' endpoint, and via names the helper in its rules' titles.
func (rs *resolverScanner) privateOps(
	files []*source,
	methods [][]resolverMethod,
) map[string]operation {
	g := rs.indexCalls(files, methods)
	keys := slices.Sorted(maps.Keys(g.private))
	for changed := true; changed; {
		changed = false
		for _, key := range keys {
			if _, done := g.ops[key]; done {
				continue
			}
			if op, ok := agreedOp(g.callers[key], g.ops); ok {
				op.via = g.private[key]
				g.ops[key] = op
				changed = true
			}
		}
	}
	served := make(map[string]operation, len(g.private))
	for key := range g.private {
		if op, ok := g.ops[key]; ok {
			served[key] = op
		}
	}
	return served
}

// callGraph is how resolver methods call each other on their receiver;
// keys are "recv.method".
type callGraph struct {
	ops     map[string]operation // operation of each method known so far
	callers map[string][]string  // callee → caller keys
	private map[string]string    // private method key → method name
}

// indexCalls indexes the resolver methods: public ones with their
// operation, private ones still to resolve.
func (rs *resolverScanner) indexCalls(files []*source, methods [][]resolverMethod) callGraph {
	g := callGraph{
		ops:     make(map[string]operation),
		callers: make(map[string][]string),
		private: make(map[string]string),
	}
	for i, fileMethods := range methods {
		for j := range fileMethods {
			m := &fileMethods[j]
			key := m.recv + "." + m.method
			if m.private {
				g.private[key] = m.method
			} else {
				op, field := rs.operationOf(files[i].name, m)
				if isEntryPoint(m.typeName) && field.file != "" {
					op.route = routeFact(files[i].name, m, &field).ID
				}
				g.ops[key] = op
			}
			for _, callee := range m.calls {
				g.callers[m.recv+"."+callee] = append(g.callers[m.recv+"."+callee], key)
			}
		}
	}
	return g
}

// agreedOp returns the one operation of the callers that have one, false
// when they name two or none has one yet.
func agreedOp(callerKeys []string, ops map[string]operation) (operation, bool) {
	var agreed operation
	for _, key := range callerKeys {
		op, ok := ops[key]
		switch {
		case !ok:
			continue
		case agreed.name == "":
			agreed = op
		case op.name != agreed.name:
			return operation{}, false
		}
	}
	return agreed, agreed.name != ""
}

// fieldFacts derives the facts of a resolver method whose schema field is
// known: its domain, its documentation and, for a root field, its endpoint
// (op.route is set to it).
func (rs *resolverScanner) fieldFacts(
	file string,
	m *resolverMethod,
	field *schemaField,
	op *operation,
) []Fact {
	facts := []Fact{domainFact(file, m.start, m.end, op.domain)}
	if field.doc != "" {
		facts = append(facts, docFact(file, m, op.name+": "+field.doc))
	}
	if isEntryPoint(m.typeName) {
		route := routeFact(file, m, field)
		op.route = route.ID
		facts = append(facts, route)
	}
	return facts
}

// skippedDirs hold no code the spec cites: dependencies, generated code and
// test fixtures.
func skippedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", "generated", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// goSources lists the service's non-test Go files, sorted.
func goSources(fsys fs.FS) ([]string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name != "." && skippedDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list Go sources: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// source is one parsed Go file of the service.
type source struct {
	name string
	fset *token.FileSet
	file *ast.File
}

// parseGo reads and parses one Go file.
func parseGo(fsys fs.FS, name string) (source, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return source{}, fmt.Errorf("read: %w", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		return source{}, fmt.Errorf("parse: %w", err)
	}
	return source{name: name, fset: fset, file: file}, nil
}

// parseSources parses the named files once for every scan; files that do
// not parse are reported and skipped.
func parseSources(fsys fs.FS, names []string) ([]source, []string) {
	var (
		sources  []source
		warnings []string
	)
	for _, name := range names {
		s, err := parseGo(fsys, name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skip %s: %v", name, err))
			continue
		}
		sources = append(sources, s)
	}
	return sources, warnings
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

// domainOf names a domain after a file: auth_emails.graphql → auth-emails,
// resolvers/user_district_info.go → user-district-info.
func domainOf(name string) string {
	return slugify(strings.TrimSuffix(path.Base(name), path.Ext(name)))
}

// docFact attaches a field's schema documentation to its resolver method.
func docFact(file string, m *resolverMethod, text string) Fact {
	return Fact{
		Kind:      kindDoc,
		ID:        fmt.Sprintf("doc.webapp.%s:%d", file, m.start),
		Attrs:     map[string]string{attrText: text},
		Source:    Ref{Path: file, Lines: lines(m.start, m.end)},
		Origin:    origin,
		Certainty: certain,
	}
}

// domainFact assigns lines start-end of file to domain.
func domainFact(file string, start, end int, domain string) Fact {
	return Fact{
		Kind:      kindDomain,
		ID:        fmt.Sprintf("domain.webapp.%s:%d-%d", file, start, end),
		Attrs:     map[string]string{attrDomain: domain},
		Source:    Ref{Path: file, Lines: lines(start, end)},
		Origin:    origin,
		Certainty: certain,
	}
}

// isRootType reports whether a GraphQL type is a root operation type:
// its fields are API entry points, so each becomes an endpoint.
func isRootType(typeName string) bool {
	switch typeName {
	case gqlQuery, "Mutation", "Subscription":
		return true
	}
	return false
}

// gqlQuery is GraphQL's query root type.
const gqlQuery = "Query"

// isEntryPoint reports whether a resolver type's fields are endpoints: the
// root types', and the entity finders, which the gateway's _entities query
// calls for any service that references the type.
func isEntryPoint(typeName string) bool {
	return isRootType(typeName) || typeName == entityType
}

// operation is what a resolver method's rules are about.
type operation struct {
	name   string // "Mutation.addAuthEmail"
	domain string
	route  string // route fact ID; "" for a non-root field
	via    string // the private helper the rules are found in; "" if none
}

// routeFact makes a root field an endpoint: method is the operation type,
// path the field, handler the resolver method in file.
func routeFact(file string, m *resolverMethod, field *schemaField) Fact {
	return Fact{
		Kind: kindRoute,
		ID:   "route.graphql." + strings.ToLower(m.typeName) + "." + field.name,
		Attrs: map[string]string{
			attrMethod:      strings.ToUpper(m.typeName),
			attrPath:        field.name,
			attrHandler:     m.recv + "." + m.method,
			attrHandlerPath: file,
		},
		Source:    Ref{Path: field.file, Lines: lines(field.line, field.line)},
		Origin:    origin,
		Certainty: certain,
	}
}

// The attributes every extractor rule shares: the EARS pattern of a rule
// that always holds, and the nature of an access rule.
const (
	earsUbiquitous = "ubiquitous"
	natureBusiness = "business"
)

// ruleFacts turns each guard of m into a rule fact for op.
func ruleFacts(
	fset *token.FileSet,
	w *wording,
	file string,
	m *resolverMethod,
	op *operation,
) []Fact {
	facts := make([]Fact, 0, len(m.guards))
	seen := make(map[string]int) // slug prefix → guards so far
	for i := range m.guards {
		g := &m.guards[i]
		k := kindOf(w, g.kind)
		slug := slugify(k.prefix + op.name)
		title := fmt.Sprintf(k.titleFmt, op.name)
		if op.via != "" {
			slug = slugify(k.prefix + op.name + " via " + op.via)
			title += " (via " + op.via + ")"
		}
		if n := seen[k.prefix]; n > 0 {
			slug += "-" + strconv.Itoa(n+1)
			title += " (" + strconv.Itoa(n+1) + ")"
		}
		seen[k.prefix]++
		ears := "unwanted"
		if g.kind != ruleAccess && g.kind != ruleRateLimit {
			ears = earsUbiquitous
		}
		attrs := map[string]string{
			attrSlug:        slug,
			attrTitle:       title,
			attrRequirement: w.requirement(fset, g, op.name),
			attrEARS:        ears,
			attrNature:      natureBusiness,
			attrDomain:      op.domain,
		}
		if op.route != "" {
			attrs[attrRoute] = op.route
		}
		facts = append(facts, Fact{
			Kind:      kindRule,
			ID:        fmt.Sprintf("rule.webapp.%s.%s:%d-%d", k.id, file, g.start, g.end),
			Attrs:     attrs,
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

// kindText is how a rule kind names its rules.
type kindText struct {
	prefix   string // slug prefix
	titleFmt string // %s operation
	id       string // fact id segment
}

// kindOf returns the naming of a rule kind. Access, open-access and
// rate-limit rules keep the "authz" id segment they always had.
func kindOf(w *wording, kind ruleKind) kindText {
	switch kind {
	case ruleRateLimit:
		return kindText{prefix: "ratelimit-", titleFmt: w.rateTitleFmt, id: "authz"}
	case ruleVisibility:
		return kindText{prefix: "visibility-", titleFmt: w.visTitleFmt, id: "visibility"}
	case ruleChecked:
		return kindText{prefix: "authzcheck-", titleFmt: w.checkTitleFmt, id: "authzcheck"}
	case ruleUnchecked:
		return kindText{prefix: "unchecked-", titleFmt: w.uncheckedTitleFmt, id: "unchecked"}
	case rulePermit:
		return kindText{prefix: "policy-", titleFmt: w.policyTitleFmt, id: "policy"}
	case ruleAccess, ruleOpen:
	}
	return kindText{prefix: "authz-", titleFmt: w.titleFmt, id: "authz"}
}
