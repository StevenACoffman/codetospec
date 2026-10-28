# SCIP converter

Turns a [SCIP](https://github.com/sourcegraph/scip) index (a code
intelligence protocol) into codetospec facts (`codetospec/facts/v1`). It emits
`symbol` facts with the **exact bounds** an indexer resolved — far
more precise than tree-sitter. That is the whole point: exact symbol
locations let the map cite the right lines instead of
guessing (blank lines, type declarations without logic).

Only citable definitions are emitted: functions, methods, types and
package-level terms. Local variables, parameters, type parameters,
packages and struct fields are dropped (on `webapp/services/users`:
46,991 → 9,056 symbols) — the map would list them in every prompt.

The facts carry `certainty: proved` and a `precise: true` attribute; the
binary injects these definitions (name, bounds, signature) into the
chunk's context during the map.

## Requirements

A SCIP index `index.scip`, produced by a language indexer — never by
this tool:

```sh
# Go
go install github.com/scip-code/scip-go/cmd/scip-go@latest
scip-go --output index.scip ./...

# TypeScript: scip-typescript · C/C++: scip-clang · etc.
```

## Usage

```sh
cd extractors/scip && go build -o scip2facts .
./scip2facts --index index.scip --root . > scip.facts.json
codetospec run --src . --out ./spec --facts scip.facts.json
```

`--root` strips a prefix from document paths. Paths outside the repository
(stdlib, dependencies, build artifacts) are ignored.

Separate Go module (depends on `github.com/scip-code/scip` and protobuf), outside
the main binary's dependency set.
