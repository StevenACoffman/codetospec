# Datastore kinds from the type checker, and shared concepts, 2026-11-12

Only 9 of the 30 outputs drew a concept map after the remap of 2026-11-11.
The proposal is built from tables and owned API types, and two gaps hid
them:

- The webapp extractor read `datastore.RegisterKind("Kind", new(T))` only
  with a string literal. webapp's `districts` service passes a named
  constant in 12 of its 50 calls (`District`, `School`,
  `ClassroomDistrictInfo`, `UserDistrictInfo`, `MetaDistrict`, …), and
  each was missing.
- Nothing read districts-jobs's own convention, so the modules, which use
  `pkg/models`, had no kinds at all. The convention is a package variable
  `<T>Kind = Kind{"Name"}` (or `RegisterKind`) beside the struct `T`;
  44 kinds, 2 of them stored under another name.

## The change

- **The Go extractor reads every kind with the type checker**
  (`extractors/go/kinds.go`). A kind is a package variable whose type is
  a struct named `Kind` holding one string, set by:
  - `RegisterKind(k, new(T))`, from any package;
  - or `Kind{k}`, the model then the struct named like the variable
    without `Kind`.

  k is read as a constant value, so a named constant is read too.
- **Relationships** (`extractors/go/keyrefs.go`). A single `*Key` field
  the code assigns a key built from a kind variable refers to that kind,
  `null`, as a reference, never ownership. The key is built:
  - in place: `NameKey(V…)` or `NameKey(V.Value…)`, IDKey, IncompleteKey;
  - or by a function that returns one.

  A key of the model's own kind is its identity, not a link, and is left
  out. So are key slices: a reference carries no multiplicity, and `n:1`
  would be false.
- **Shared kinds.** A module gets the shared root's kinds it reaches, by
  their variable or model type (`SharedKindFacts`), cited under the root.
- **One producer per kind.** The webapp extractor no longer emits kinds.
- **README.** An output with nothing to propose concepts from says so
  under the concept map heading (`render.Meta.ConceptMapNone`).

## Revisions and commands

- codetospec branch `datastore-kinds`, on `packagemap-simplify` (#27).
- webapp `c55b04b0090`, districts-jobs `dde732546`, Go 1.27.4.
- Each webapp output was rerun with `scanwebapp.sh <svc> --quality
  --crosscheck --acceptance --write-domain-model`, then
  `scandistricts.sh all` with the same flags. Every phase came from the
  caches: 0 tokens in all 30 runs.
- `make corpus`; `export --format anarch` for the six webapp services;
  `anarch-lint proposals report` on webapp for districts.

## Results

Rules are unchanged in every output (entities are outside every prompt
key, and the reduce key hashes the candidate rules), and the corpus
passes.

| output                                              | kinds      | shared kinds | references | concept map edges |
| --------------------------------------------------- | ---------- | ------------ | ---------- | ----------------- |
| districts                                           | 43 → 56    | 0            | 0 → 8      | 0 → 9             |
| districts-jobs pkg                                  | 0 → 44     | 0            | 0 → 6      | 68 → 74           |
| districts-jobs roster                               | 0 → 23     | 23           | 0 → 3      | 18 → 21           |
| districts-jobs admin-reports                        | 0 → 20     | 20           | 0 → 1      | none → 2          |
| districts-jobs khanx                                | 0 → 14     | 14           | 0 → 1      | none → 2          |
| districts-jobs lms-connect                          | 0 → 8      | 8            | 0 → 1      | none → 2          |
| districts-jobs distutil                             | 0 → 7      | 7            | 0          | none → 0          |
| districts-jobs pull-test-results                    | 0 → 6      | 6            | 0          | none → 0          |
| districts-jobs yearend                              | 0 → 4      | 4            | 0          | none → 0          |
| districts-jobs alerter                              | 0 → 2      | 2            | 0          | none → 0          |
| districts-jobs khanxadmin, listdistricts, lockwatch | 0 → 1 each | 1            | 0          | none → 0          |
| webapp pkg                                          | 0 → 3      | 0            | 0          | none → 0          |

- Concept maps: 9 → 20 outputs. The other 10 are districts-jobs modules
  that use no kind and no table (deployer, instructional-area-gen,
  isyncso, moa-transitions, pull-demographics, repoman,
  rosterjob-updates, safelister, signer, teller), and each README now
  says so.
- The other webapp services kept their kind counts: their kinds were
  already found, now by the Go extractor.
- districts' anarch proposals: 24 → 43, its recovered kinds' enumerated
  fields. All 43 pass `anarch-lint proposals report` on today's code.
- The first refresh flagged self-references: every model whose own
  `Key` field is assigned its kind's key. They were excluded before the
  run recorded here.

## Limits

- References come only from code assigning a constructed key, so they
  are few: 8 in districts, 6 in districts-jobs pkg. A key assigned from a
  loaded entity, a parameter or a slice is not followed.
- A builder is followed one hop.
- Kinds now depend on the Go extractor loading the packages. A failed
  load drops them, where the AST-only webapp extractor kept them.
- A shared kind is "used" when the module reaches its variable or model
  type at all. That is transitive, and interface calls are not followed.
- The SQL route (tables named in sqlc's generated code) stays deferred:
  it would help 3 modules and needs call-level evidence.
