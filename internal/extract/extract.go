// Package extract defines the Fact model shared by all extraction layers,
// the merge/dedup logic and the domain resolution rules.
package extract

import (
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/sitter"
)

// Fact is a mechanically provable statement about the codebase.
type Fact struct {
	Kind      string            `json:"kind"` // "symbol", "route", "table", "module", "import", KindRule, KindDomain
	ID        string            `json:"id"`   // stable identifier
	Attrs     map[string]string `json:"attrs"`
	Source    Ref               `json:"source"`
	Origin    string            `json:"origin"`    // "sitter" | extractor name | facts file
	Certainty string            `json:"certainty"` // "proved" | "static"
}

// ViaDispatch reports whether a call or test fact's callee is reached only
// through an interface (AttrDispatch): each consumer decides whether such
// a possible use counts.
func (f *Fact) ViaDispatch() bool { return f.Attrs[AttrDispatch] != "" }

// Fact kinds that external extractors may emit beyond the structural ones
// ("symbol", "route", "table", "module", "import"). This block is the single
// authoritative description of their contract.
const (
	// KindRule is a business rule an extractor derived deterministically from
	// the code: Source is its exact citation, and the attributes AttrSlug,
	// AttrTitle, AttrRequirement (EARS text in the output language), AttrEARS
	// (EARS kind), AttrNature and AttrDomain describe it. The graph turns
	// valid rule facts into rule nodes that skip the LLM crosscheck. The
	// optional AttrRoute names the route fact (by ID) of the endpoint the rule
	// guards; it becomes an exposed_by edge.
	KindRule = "rule"
	// KindDomain assigns the code in Source to the domain AttrDomain,
	// overriding the configured domain strategy for chunks starting there.
	KindDomain = "domain"
	// KindConstant binds the identifier AttrName (as written in code, e.g.
	// "http.StatusBadRequest" or "maxRetries") to the numeric AttrValue, so
	// grounding finds a stated "400" behind the constant's name.
	KindConstant = "constant"
	// KindDoc attaches authored documentation AttrText (e.g. an API schema
	// description) to the code in Source; the map prompt shows it for the
	// chunks it overlaps.
	KindDoc = "doc"
	// KindDependency records that the code at Source calls AttrOperation,
	// owned by the external service AttrService (a slug), optionally with
	// the authority AttrAs (AsServiceAdmin, AsUser). The graph gets one
	// service node per service and depends_on edges from calling domains.
	KindDependency = "dependency"
	// KindRoute is an endpoint: Attrs path, method, handler, AttrHandlerPath
	// and AttrHandlerLines.
	KindRoute = "route"
	// The structural kinds every layer emits.
	KindModule = "module"
	KindImport = "import"
	KindTable  = "table"
	KindSymbol = "symbol"
	// KindReach marks code of a shared root (Source.Root) that the
	// analyzed code reaches: a declaration it calls or uses, directly or
	// through other shared code. Rules of the shared root's own spec that
	// cite reached code are inherited (see package inherit).
	KindReach = "reach"
	// KindCall records that the definition at Source (the caller) calls or
	// uses the definition at AttrCalleePath, AttrCalleeLines (the callee),
	// both in --src. A definition the callers of one domain alone use is
	// placed in that domain (see package callers). A fact marked
	// AttrDispatch is a call through an interface: package callers and
	// drift's callees ignore it, package testreach counts it apart.
	KindCall = "call"
	// KindTest records that the test function at Source (AttrName, e.g.
	// TestClose) uses the definition at AttrCalleePath, AttrCalleeLines,
	// itself or through helpers of its test files. With the call facts, it
	// tells which rules' cited code tests reach (see package testreach).
	KindTest = "test"
	// KindEnum is an enumeration: the type at Source (AttrName, e.g.
	// "shop.Status") and its values, one KindEnumValue fact each
	// (AttrEnum = the enum fact's ID, AttrName, AttrValue, Source = the
	// constant). A KindTransition fact (AttrEnum, AttrFrom, AttrTo: value
	// names) is code at Source that, while a value of the enumeration is
	// AttrFrom, sets it to AttrTo. The graph gets one enum node each.
	KindEnum       = "enum"
	KindEnumValue  = "enum_value"
	KindTransition = "transition"
	// KindExecuted is what a test run executed in the file at Source.Path,
	// read from a coverage profile: AttrExecuted lists the lines it ran,
	// AttrUnexecuted the statement lines it did not, each as
	// comma-separated "a-b" spans. Observed, unlike the reach of test
	// facts, and only for the code of the commit the profile was made on:
	// the extractor leaves out the files changed since.
	KindExecuted = "executed"
)

// DispatchInterface is AttrDispatch on a call or test fact whose caller
// calls a method of an interface of the module, and whose callee is one
// of the module's implementations of it: a use that may happen, not one
// that is known to.
const DispatchInterface = "interface"

// Values of AttrAs.
const (
	AsServiceAdmin = "service-admin"
	AsUser         = "user"
)

// Attribute names of KindRule and KindDomain facts.
const (
	AttrSlug        = "slug"
	AttrTitle       = "title"
	AttrRequirement = "requirement"
	AttrEARS        = "ears"
	AttrNature      = "nature"
	AttrDomain      = "domain"
	AttrName        = "name"
	AttrPath        = "path"      // a route's path
	AttrMethod      = "method"    // a route's HTTP method
	AttrNamespace   = "namespace" // the namespace a fact sits in
	AttrTarget      = "target"    // what an import names
	AttrColumns     = "columns"   // a table's "name:type" columns, comma-separated
	AttrValue       = "value"
	AttrRoute       = "route"
	AttrText        = "text"
	// AttrDescription, on a table fact, is the entity's authored
	// description (a struct's doc comment, a schema comment): what the
	// entity is, in its authors' words. The entity page shows it.
	AttrDescription = "description"
	AttrService     = "service"
	AttrOperation   = "operation"
	// AttrInheritedFrom, on a rule fact, names the spec the rule comes
	// from: an LLM rule of shared code (webapp's pkg/ spec) the extractor
	// found the analyzed code reaches. It then carries that spec's
	// AttrCrosscheck verdict, AttrOrigin and AttrConfidence, and is never
	// reviewed again (its citation is outside --src).
	AttrInheritedFrom = "inherited_from"
	AttrCrosscheck    = "crosscheck"
	// AttrStaleVerdict, on an inherited rule fact, is the verdict the
	// shared spec gave on code that has changed since: the fact's
	// AttrCrosscheck is then unchecked.
	AttrStaleVerdict = "stale_verdict"
	AttrOrigin       = "origin"
	AttrConfidence   = "confidence"
	// AttrAs, on a dependency fact, is the authority the call is made
	// with: AsServiceAdmin (webapp's service-admin queries, which bypass
	// user permission checks that accept them) or AsUser (the caller's
	// own); absent when the extractor cannot tell.
	AttrAs = "as"
	// AttrHandlerPath, on a "route" fact, is the file holding the endpoint's
	// handler: its chunks may cite the endpoint. It beats matching the handler
	// name against symbols.
	AttrHandlerPath = "handler_path"
	// AttrHandlerLines, with AttrHandlerPath, is the span of the handler's
	// code ("a-b"): a rule citing it is linked to the endpoint (exposed_by)
	// without asking the LLM.
	AttrHandlerLines = "handler_lines"
	// AttrCalleePath and AttrCalleeLines, on a call or test fact, cite the
	// callee.
	AttrCalleePath  = "callee_path"
	AttrCalleeLines = "callee_lines"
	// AttrDispatch, on a call or test fact, says how the callee is reached
	// when it is not used directly: DispatchInterface, through a method of
	// an interface the callee implements (see Fact.ViaDispatch).
	AttrDispatch = "dispatch"
	// AttrExecuted and AttrUnexecuted, on an executed fact.
	AttrExecuted   = "executed"
	AttrUnexecuted = "unexecuted"
	// AttrEnum, AttrFrom and AttrTo, on enum_value and transition facts.
	AttrEnum = "enum"
	AttrFrom = "from"
	AttrTo   = "to"
)

// Ref pins a fact to a file location.
type Ref struct {
	Path  string `json:"path"`  // relative to --src (or to Root), slash-separated
	Lines string `json:"lines"` // "12-87", 1-based inclusive
	// Root names the source tree Path is relative to, when it is not
	// --src: one given by --ref-root (see Roots). Only an extractor's
	// mechanical rules cite one; chunks and LLM citations never do.
	Root string `json:"root,omitempty"`
}

// Certainty and origin constants used across the pipeline.
const (
	CertaintyProved = "proved"
	CertaintyStatic = "static"
	OriginSitter    = "sitter"
)

// Merge concatenates fact sets and deduplicates by ID with priority
// proved > external extractor > sitter. On equal priority the first
// occurrence wins. The result is sorted by ID for determinism.
func Merge(sets ...[]Fact) []Fact {
	byID := make(map[string]Fact)
	var order []string
	for _, set := range sets {
		for _, f := range set {
			existing, ok := byID[f.ID]
			if !ok {
				byID[f.ID] = f
				order = append(order, f.ID)
				continue
			}
			if factRank(&f) > factRank(&existing) {
				byID[f.ID] = f
			}
		}
	}
	sort.Strings(order)
	merged := make([]Fact, 0, len(order))
	for _, id := range order {
		merged = append(merged, byID[id])
	}
	return merged
}

// Fact precedence ranks, highest first.
const (
	rankProved    = 3 // runtime-proved
	rankExtractor = 2 // an external extractor's static fact
	rankSitter    = 1 // the universal tree-sitter layer
)

// factRank orders fact precedence: proved beats external extractors,
// which beat the universal tree-sitter layer.
func factRank(f *Fact) int {
	switch {
	case f.Certainty == CertaintyProved:
		return rankProved
	case f.Origin != OriginSitter:
		return rankExtractor
	default:
		return rankSitter
	}
}

// DomainResolver derives domain slugs according to the configured strategy.
type DomainResolver struct {
	Strategy string // "auto" | "namespace" | "directory"
	// Depth is how many namespace segments (after the root prefix) form the
	// domain. 0 or 1 keeps a single segment ("core"); 2 gives
	// "core-controller", 3 "core-controller-sdk" — useful when a whole repo
	// lives under one root namespace and would otherwise collapse to one
	// mega-domain.
	Depth int
	// Ambiguous lists namespaces declared in several directories (see
	// AmbiguousNamespaces): they name no single area, so files in them are
	// resolved by their directory instead.
	Ambiguous map[string]bool
}

// Resolve returns the domain slug for a namespace/path pair.
func (r DomainResolver) Resolve(namespace, path string) string {
	strategy := r.Strategy
	if strategy == "" {
		strategy = "auto"
	}
	if r.Ambiguous[namespace] {
		return domainFromDir(path)
	}
	if strategy != "directory" && namespace != "" {
		if d := domainFromNamespace(namespace, r.Depth); d != "" {
			return d
		}
	}
	return domainFromPath(path)
}

// AmbiguousNamespaces returns the namespaces that files in more than one
// directory declare. Go names every command's package "main" and reuses
// names like "util" across a tree; such a namespace says nothing about the
// area a file belongs to. It reads the per-file tree-sitter information,
// as namespaces are resolved from it: "module" facts cannot tell, since
// merging keeps one per name.
func AmbiguousNamespaces(files map[string]*sitter.FileInfo) map[string]bool {
	dirs := make(map[string]map[string]bool)
	for path, info := range files {
		if info == nil {
			continue
		}
		for _, m := range info.Modules {
			if dirs[m.Name] == nil {
				dirs[m.Name] = make(map[string]bool)
			}
			dirs[m.Name][pathpkg.Dir(path)] = true
		}
	}
	ambiguous := make(map[string]bool)
	for name, in := range dirs {
		if len(in) > 1 {
			ambiguous[name] = true
		}
	}
	return ambiguous
}

// domainFromDir names a domain after a file's whole directory:
// cmd/seedroster/main.go → cmd-seedroster.
func domainFromDir(path string) string {
	dir := pathpkg.Dir(strings.TrimPrefix(path, "./"))
	if dir == "." || dir == "/" {
		return "root"
	}
	return Slugify(strings.ReplaceAll(dir, "/", "-"))
}

// DomainOf derives the domain slug for a fact using the auto strategy:
// namespace attribute when available, first directory under --src otherwise.
func DomainOf(f *Fact, path string) string {
	return DomainResolver{Strategy: "auto"}.Resolve(f.Attrs["namespace"], path)
}

// domainFromNamespace joins the `depth` segments after the common root
// prefix (first segment) of a qualified namespace, lowercased and slugified.
func domainFromNamespace(namespace string, depth int) string {
	segments := SplitQualified(namespace)
	if len(segments) == 0 {
		return ""
	}
	if len(segments) == 1 {
		return Slugify(segments[0])
	}
	if depth < 1 {
		depth = 1
	}
	start := 1
	end := min(start+depth, len(segments))
	return Slugify(strings.Join(segments[start:end], "-"))
}

// domainFromPath picks the first directory under the source root.
func domainFromPath(path string) string {
	path = strings.TrimPrefix(strings.TrimPrefix(path, "./"), "/")
	if i := strings.IndexByte(path, '/'); i > 0 {
		return Slugify(path[:i])
	}
	return "root"
}

var qualifiedSeparators = regexp.MustCompile(`\\+|::|/`)

// SplitQualified splits a qualified name (PHP namespace, Go import path,
// Rust module path) into its segments.
func SplitQualified(name string) []string {
	parts := qualifiedSeparators.Split(name, -1)
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			segments = append(segments, p)
		}
	}
	return segments
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify lowercases s and collapses non-alphanumeric runs into dashes.
func Slugify(s string) string {
	s = nonSlug.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(s, "-")
}

var linesFormat = regexp.MustCompile(`^(\d+)-(\d+)$`)

// ParseLines parses a "A-B" 1-based inclusive line range.
func ParseLines(s string) (int, int, error) {
	m := linesFormat.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, fmt.Errorf("invalid line range %q, want \"A-B\"", s)
	}
	a, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid line range %q: %w", s, err)
	}
	b, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid line range %q: %w", s, err)
	}
	if a < 1 || b < a {
		return 0, 0, fmt.Errorf("invalid line range %q: bounds out of order", s)
	}
	return a, b, nil
}
