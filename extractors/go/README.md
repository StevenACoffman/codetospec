# Native Go extractor

Native codetospec extractor for the Go ecosystem, on the
`codetospec/facts/v1` protocol. It emits what tree-sitter cannot see: HTTP
**routes**, numeric **constants** and SQL **tables**.

## What it extracts

- **Routes** (`route`) via `golang.org/x/tools/go/packages` (resolved types):
  gin/echo (`r.GET("/x", h)`), chi (`r.Get(...)`), net/http
  (`mux.HandleFunc("GET /x", h)`), plus `Handle(method, path, …)` and
  `Any`. Group prefixes (`api := r.Group("/api")`) are resolved, and
  the handler is named from type information (`pkg.Func`,
  `Type.Method`). Its code is located (`handler_path`, `handler_lines`:
  the function's declaration) through the common registration forms:
  function or method, a closure that calls a single function
  (`func(w, r) { handleX(w, r) }`), a `http.HandlerFunc(h)` conversion,
  a wrapper that receives the handler (`withAuth(h)`); any other closure is
  itself the handler. codetospec then links to the endpoint any rule that
  cites this code. A handler outside the module (`playground.Handler(…)`) has
  the registration itself as its code, the closest code that serves the route.
  `-skip-dirs testutil` (comma-separated list, empty by default)
  drops the routes registered under these folders, at any depth:
  the fake servers of test helpers (`testutil/hcaptcha.go`) are not
  entry points. `scanwebapp.sh` passes it.
- **Constants** (`constant`): each numeric constant the module's code
  uses, named as the code writes it (`h.StatusBadRequest`
  with an aliased import, bare `maxRetries`), with its exact value
  (`go/constant`), standard library included. Grounding thus
  finds a stated "400" behind `http.StatusBadRequest`. A bare name bound to
  two values (two packages) is dropped.
- **Precise symbols** (`symbol`, `precise: true`): each function,
  method and named type of the module, with its exact span, kind and
  signature; same ids as the tree-sitter layer and the SCIP converter
  (they merge). Makes scip-go unnecessary for Go.
- **Calls** (`call`): which function or method of the module calls or
  uses which other declaration of the module (one pair per fact, each
  cited by its span); codetospec places a definition used by the
  callers of a single domain in that domain. A call `x.M()` on an
  interface of the module gives one fact per implementation of `M` in the
  module, marked `dispatch: interface` (a possible use, not a certain one);
  a direct use of the same pair wins. Interfaces of other
  modules (`error`, `io.Writer`) are not followed.
- **Tests** (`test`): what each `TestXxx`/`FuzzXxx`, or
  suite method `TestXxx()`, uses, itself or through the helpers of its
  `_test.go` files; the same `dispatch: interface` links as calls.
- **Enumerations** (`enum`, `enum_value`, `transition`): a named string
  or integer type with at least two constants (generated files excluded),
  its values, and the guarded state changes (`x = B` under `x == A`).
- **Executed lines** (`executed`, with `-cover profile`): for each
  file of the module, the lines the profiled test run executed
  and those it did not (`go test -coverpkg=./...
  -coverprofile=profile ./...`, at the same commit). A file modified after
  the profile is dropped (warning): its lines no longer match.
- **Reached shared code** (`reach`, with `-shared name=dir`, repeatable):
  each declaration of the shared module (`go.mod` of `dir`) that the
  analyzed module reaches — what its code names, then what those declarations
  name; a reached type reaches its methods; interfaces are not
  followed (lower bound). Cited under the root `name` (`@name/path`),
  for `codetospec run --inherit name=… --ref-root name=…`. The module must
  resolve the shared code locally (go.work or `replace`).
- **Tables** (`table`): `CREATE TABLE` in `.sql` files (sqlc schema,
  migrations), with typed columns; table constraints are ignored.

## Requirements

The target module must be loadable by `go/packages` (Go toolchain
present, resolvable dependencies). Partial compilation errors
are tolerated: extraction continues with the type information available.

## Usage

```sh
cd extractors/go && go build -o extract .
```

`codetospec.yaml` entry of the analyzed repository:

```yaml
extractors:
  - name: go
    cmd: go
    args: [run, ./extractors/go, --root, "{src}"]
    timeout: 300s
```

Option `--sql-glob "sql/schema/*.sql"` to restrict the schema files
scanned; `--cover profile` for executed lines.
