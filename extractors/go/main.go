// Command extract is the native Go facts extractor for codetospec. It speaks
// the codetospec/facts/v1 protocol: it prints a facts JSON envelope on stdout
// and free logs on stderr, so the codetospec binary consumes it without
// knowing anything about Go.
//
// It emits what tree-sitter cannot: `route` facts (gin/echo/chi/net-http,
// with group prefixes and handlers resolved via go/packages type info),
// `constant` facts (numeric constants the code refers to, for grounding),
// precise `symbol` facts and `call` facts (who uses whom, for domains),
// `test` facts (what each test function uses, for the rules tests reach),
// `enum`, `enum_value` and `transition` facts (enumerations and the state
// changes the code guards), `table` facts (SQL CREATE TABLE from schema
// files, e.g. sqlc) and, given a coverage profile, `executed` facts (the
// lines the profiled tests ran).
//
// Usage: extract --root <src> [--sql-glob "sql/schema/*.sql"] [--cover <profile>]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// excludedDir reports whether a directory named name holds no schema of
// the module: vendored, generated or build output.
func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", "testdata", "dist", "build", ".git", "tmp":
		return true
	}
	return false
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Exit codes of run.
const (
	exitOK    = 0
	exitError = 1
)

// logf writes one diagnostic line; stderr has nowhere to report a failure.
type logf func(format string, a ...any)

// run is the whole program behind main: it parses args, extracts the facts
// of the module and writes the envelope to stdout.
func run(args []string, stdout, stderr io.Writer) int {
	log := logf(func(format string, a ...any) {
		_, _ = fmt.Fprintf(stderr, "extract: "+format+"\n", a...)
	})
	flags := flag.NewFlagSet("extract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "module root to analyze (required)")
	sqlGlob := flags.String(
		"sql-glob",
		"",
		"optional glob (relative to root) restricting SQL schema files",
	)
	skipDirs := flags.String(
		"skip-dirs",
		"",
		"comma-separated directory names whose route registrations are not entry points "+
			"(e.g. testutil, where test helpers register mock servers)",
	)
	coverProfile := flags.String(
		"cover",
		"",
		"optional coverage profile of the module's tests (go test -coverpkg=./... "+
			"-coverprofile=FILE): emit the lines each file's tests executed",
	)
	var sharedSpecs nameDirs
	flags.Var(&sharedSpecs, "shared",
		"name=dir of shared code the module builds on (repeatable): emit a reach fact for "+
			"each declaration of it the module reaches, cited under the root name")
	if err := flags.Parse(args); err != nil {
		return exitError
	}
	if *root == "" {
		log("--root is required")
		return exitError
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		log("%v", err)
		return exitError
	}
	shared, err := sharedRoots(sharedSpecs)
	if err != nil {
		log("%v", err)
		return exitError
	}

	var facts []Fact
	if m, ok := loadPackages(absRoot, log); ok {
		facts, err = moduleFacts(&m, absRoot, &options{
			skipDirs: dirSet(*skipDirs),
			shared:   shared,
			cover:    *coverProfile,
		}, log)
		if err != nil {
			log("%v", err)
			return exitError
		}
	}
	facts = append(facts, tableFacts(absRoot, *sqlGlob, log)...)

	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	if encodeErr := json.NewEncoder(stdout).
		Encode(Envelope{Schema: schemaID, Facts: facts}); encodeErr != nil {
		log("encode: %v", encodeErr)
		return exitError
	}
	return exitOK
}

// options are the extraction choices besides the root.
type options struct {
	skipDirs map[string]bool // directories whose routes are not entry points
	shared   []SharedRoot    // shared code to emit reach facts for
	cover    string          // coverage profile path, "" for none
}

// moduleFacts extracts the facts of a loaded module, logging their counts.
func moduleFacts(m *loadedModule, root string, opts *options, log logf) ([]Fact, error) {
	pkgs, fset := m.pkgs, m.fset
	facts := routeFacts(pkgs, fset, root, opts.skipDirs, log)
	constants := ConstantFacts(pkgs, fset, root)
	log("%d constant(s)", len(constants))
	facts = append(facts, constants...)
	symbols, calls := SymbolFacts(pkgs, fset, root), CallFacts(pkgs, fset, root)
	log("%d symbol(s), %d call(s)", len(symbols), len(calls))
	facts = append(facts, symbols...)
	facts = append(facts, calls...)
	enums := EnumFacts(pkgs, fset, root)
	log("%d enumeration fact(s)", len(enums))
	facts = append(facts, enums...)
	tests := TestFacts(m, root)
	log("%d test fact(s)", len(tests))
	facts = append(facts, tests...)
	if opts.cover != "" {
		executed, err := executedFacts(opts.cover, m, root, log)
		if err != nil {
			return nil, err
		}
		facts = append(facts, executed...)
	}
	for i := range opts.shared {
		reach := ReachFacts(pkgs, fset, &opts.shared[i])
		log("%d declaration(s) of %s reached", len(reach), opts.shared[i].Name)
		facts = append(facts, reach...)
	}
	return facts, nil
}

// executedFacts reads the coverage profile at path into executed facts,
// logging the files skipped as changed since the profile.
func executedFacts(path string, m *loadedModule, root string, log logf) ([]Fact, error) {
	profiles, stale, err := readProfile(path, root)
	if err != nil {
		return nil, err
	}
	facts, skipped := ExecutedFacts(profiles, profileFiles(m.pkgs, root), stale)
	log("%d file(s) with executed lines", len(facts))
	if len(skipped) > 0 {
		log("%d file(s) changed since the coverage profile, left out: %s",
			len(skipped), strings.Join(skipped, ", "))
	}
	return facts, nil
}

func dirSet(list string) map[string]bool {
	set := make(map[string]bool)
	for _, name := range strings.Split(list, ",") {
		if name = strings.TrimSpace(name); name != "" {
			set[name] = true
		}
	}
	return set
}

// inDir reports whether a slash-separated path lies under a directory
// named in dirs, at any depth.
func inDir(file string, dirs map[string]bool) bool {
	parts := strings.Split(file, "/")
	for _, dir := range parts[:len(parts)-1] {
		if dirs[dir] {
			return true
		}
	}
	return false
}

// routeFacts loads the module with type information and converts detected
// routes to facts, but those registered under a skipped directory. Load
// errors are tolerated: partial results still ship.
// nameDirs is a repeatable name=dir flag.
type nameDirs []string

func (n *nameDirs) String() string { return strings.Join(*n, ",") }

// Set records one name=dir pair, checked by sharedRoots.
func (n *nameDirs) Set(value string) error {
	*n = append(*n, value)
	return nil
}

// sharedRoots resolves -shared name=dir pairs: the directory made absolute,
// the module path read from its go.mod.
func sharedRoots(specs []string) ([]SharedRoot, error) {
	roots := make([]SharedRoot, 0, len(specs))
	for _, spec := range specs {
		name, dir, found := strings.Cut(spec, "=")
		if !found || name == "" || dir == "" {
			return nil, fmt.Errorf("-shared %q: want name=dir", spec)
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("-shared %s: %w", name, err)
		}
		data, err := os.ReadFile(filepath.Clean(filepath.Join(abs, "go.mod")))
		if err != nil {
			return nil, fmt.Errorf("-shared %s: %w", name, err)
		}
		module := modfile.ModulePath(data)
		if module == "" {
			return nil, fmt.Errorf("-shared %s: %s/go.mod declares no module", name, dir)
		}
		roots = append(roots, SharedRoot{Name: name, Dir: abs, Module: module})
	}
	return roots, nil
}

// loadedModule is the module under analysis, type-checked.
type loadedModule struct {
	// pkgs are the module's packages without their tests: every fact but
	// the test facts reads these, so loading tests changes none of them.
	pkgs []*packages.Package
	// tests are the test variants ("p [p.test]", "p_test [p.test]"),
	// which hold the _test.go files.
	tests []*packages.Package
	fset  *token.FileSet
}

// loadPackages loads the module under root, its tests included, with
// syntax and type information. A load error skips every fact that needs
// types (false); package errors only leave type information partial.
func loadPackages(root string, log logf) (loadedModule, bool) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir:   root,
		Fset:  fset,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		log("load packages: %v (routes and constants skipped)", err)
		return loadedModule{}, false
	}
	var loadErrors int
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		loadErrors += len(p.Errors)
	})
	if loadErrors > 0 {
		log("%d package load error(s), continuing with partial type info", loadErrors)
	}
	m := loadedModule{fset: fset}
	for _, p := range pkgs {
		switch {
		case strings.Contains(p.ID, " ["):
			m.tests = append(m.tests, p)
		case !strings.HasSuffix(p.ID, ".test"): // not a generated test main
			m.pkgs = append(m.pkgs, p)
		}
	}
	return m, true
}

// routeFacts turns the routes registered in pkgs into route facts.
func routeFacts(
	pkgs []*packages.Package,
	fset *token.FileSet,
	root string,
	skip map[string]bool,
	log logf,
) []Fact {
	var facts []Fact
	for _, r := range ExtractRoutes(pkgs, fset, root) {
		if inDir(r.Ref.Path, skip) {
			continue
		}
		attrs := map[string]string{"path": r.Path}
		if r.Method != "" {
			attrs["method"] = r.Method
		}
		if r.Handler != "" && r.Handler != "closure" {
			attrs["handler"] = r.Handler
		}
		if r.HandlerRef.Path != "" {
			attrs["handler_path"] = r.HandlerRef.Path
			attrs["handler_lines"] = r.HandlerRef.Lines
		}
		method := r.Method
		if method == "" {
			method = "any"
		}
		facts = append(facts, Fact{
			Kind:      "route",
			ID:        "route." + strings.ToLower(method) + "." + r.Path,
			Attrs:     attrs,
			Source:    r.Ref,
			Origin:    origin,
			Certainty: certain,
		})
	}
	log("%d route(s)", len(facts))
	return facts
}

// tableFacts scans SQL files for CREATE TABLE statements; the first file
// (in walk order) declaring a table name wins.
func tableFacts(root, sqlGlob string, log logf) []Fact {
	var facts []Fact
	seen := map[string]bool{}
	for _, rel := range sqlFiles(root, sqlGlob, log) {
		data, err := os.ReadFile(filepath.Clean(filepath.Join(root, filepath.FromSlash(rel))))
		if err != nil {
			log("read %s: %v", rel, err)
			continue
		}
		for _, t := range ParseTables(string(data)) {
			if seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			facts = append(facts, Fact{
				Kind: "table",
				ID:   "table." + t.Name,
				Attrs: map[string]string{
					attrName:  t.Name,
					"columns": strings.Join(t.Columns, ", "),
				},
				Source:    Ref{Path: rel, Lines: itoa(t.Line) + "-" + itoa(t.Line)},
				Origin:    origin,
				Certainty: certain,
			})
		}
	}
	log("%d table(s)", len(facts))
	return facts
}

// sqlFiles lists the .sql files under root, relative and slash-separated,
// in walk order: excluded and hidden directories are skipped, and so are
// files sqlGlob (when set) does not match.
func sqlFiles(root, sqlGlob string, log logf) []string {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Only directories (the root included) report errors.
			log("scan %s: %v", path, walkErr)
			return filepath.SkipDir
		}
		if d.IsDir() {
			if excludedDir(d.Name()) || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".sql") {
			return nil
		}
		rel, err := relTo(root, path)
		if err != nil {
			return err
		}
		if matched, _ := filepath.Match(sqlGlob, rel); sqlGlob == "" || matched {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		log("scan sql: %v", err)
	}
	return files
}

func relTo(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func itoa(n int) string { return strconv.Itoa(n) }
