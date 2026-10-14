// Command extract is codetospec's extractor for Khan Academy's webapp Go
// services (gqlgen GraphQL). It speaks the codetospec/facts/v1 protocol:
// facts JSON on stdout, logs on stderr.
//
// It emits what an LLM reproduces expensively and imprecisely:
//   - `rule` facts: the authorization guard of every gqlgen resolver method
//     (acl.* predicates, ktx.Request…() checks, acl.OpenAccess()), as an EARS
//     requirement naming its GraphQL operation, cited to the guard's lines;
//   - `domain` facts: each resolver method's domain, named after the schema
//     file that declares its field (auth_emails.graphql → auth-emails).
//
// Usage: extract -root <service dir> [-lang fr|en]
//
//	extract -all -root <webapp checkout> [-lang fr|en]: every Go service,
//	consolidated relative to the checkout (see AnalyzeAll).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Exit codes of run.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// run is the whole program behind main: it parses args, analyzes the
// service and writes the envelope.
func run(args []string, stdout, stderr io.Writer) int {
	// stderr is diagnostics only: a failed write there has nowhere to go.
	logf := func(format string, a ...any) { _, _ = fmt.Fprintf(stderr, "extract: "+format+"\n", a...) }
	flags := flag.NewFlagSet("extract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "webapp service directory to analyze (required)")
	lang := flags.String("lang", "fr", "requirement language: fr or en")
	supergraph := flags.String("supergraph", "",
		"directory holding composed_schema.graphql (default: <root>/../../gengraphql)")
	graph := flags.String(
		"graph",
		"",
		"this service's subgraph name (default: upper-cased root name)",
	)
	webapp := flags.String(
		"webapp",
		"",
		"webapp checkout, whose pkg/ marks shared checkers (default: <root>/../.. when it has pkg/)",
	)
	accessPkgs := flags.String("access-pkgs", "permissions",
		"comma-separated service packages whose bool functions are access checks")
	all := flags.Bool("all", false,
		"analyze every Go service of the webapp checkout -root, consolidated")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if *root == "" || (*lang != "fr" && *lang != "en") {
		logf("-root is required and -lang must be fr or en")
		return exitUsage
	}
	if *all {
		facts, warnings, err := AnalyzeAll(&AllInputs{
			Webapp:     os.DirFS(*root),
			GOROOT:     os.DirFS(build.Default.GOROOT),
			Lang:       *lang,
			AccessPkgs: strings.Split(*accessPkgs, ","),
		})
		return emit(facts, warnings, err, stdout, logf)
	}

	in := Inputs{
		Service:      os.DirFS(*root),
		GOROOT:       os.DirFS(build.Default.GOROOT),
		ServiceGraph: *graph,
		Lang:         *lang,
		AccessPkgs:   strings.Split(*accessPkgs, ","),
	}
	if in.ServiceGraph == "" {
		in.ServiceGraph = strings.ToUpper(strings.ReplaceAll(filepath.Base(*root), "-", "_"))
	}
	dir := *supergraph
	if dir == "" {
		dir = filepath.Join(*root, "..", "..", "gengraphql")
	}
	if _, statErr := os.Stat(filepath.Join(dir, composedSchemaFile)); statErr == nil {
		in.Supergraph = os.DirFS(dir)
	}
	webappDir := *webapp
	if webappDir == "" {
		webappDir = filepath.Join(*root, "..", "..")
	}
	if info, statErr := os.Stat(
		filepath.Join(webappDir, sharedDir),
	); statErr == nil &&
		info.IsDir() {
		in.Webapp = os.DirFS(webappDir)
	}
	facts, warnings, err := Analyze(&in)
	return emit(facts, warnings, err, stdout, logf)
}

// emit logs warnings and writes the envelope, returning the exit code.
func emit(
	facts []Fact,
	warnings []string,
	err error,
	stdout io.Writer,
	logf func(string, ...any),
) int {
	for _, w := range warnings {
		logf("%s", w)
	}
	if err != nil {
		logf("%v", err)
		return exitFailure
	}
	if encErr := json.NewEncoder(stdout).
		Encode(Envelope{Schema: schemaID, Facts: facts}); encErr != nil {
		logf("encode: %v", encErr)
		return exitFailure
	}
	return exitOK
}
