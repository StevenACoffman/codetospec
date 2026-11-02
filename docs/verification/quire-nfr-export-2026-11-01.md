# Quire NFR export against the published schemas — 2026-11-01

## Question

Does `codetospec export` write non-functional requirements that Quire
accepts, including those whose target the code does not state? No
`quire validate` binary is published, so the check runs the module's own
frontmatter schemas and the section rules of its NFR skeleton.

## Revisions

- codetospec: `81a5f55` plus the uncommitted NFR export of the same day.
- Schemas: agent-ix/spec-artifacts-iso 0.3.0 at `9f17d63`
  (`spec_artifacts_iso/schemas/{fr,nfr,index}-frontmatter.schema.json`,
  `spec_artifacts_iso/skeletons/nfr.md`), downloaded beside the script
  with `gh api repos/agent-ix/spec-artifacts-iso/contents/<path>?ref=9f17d63`.
- Documents: the export goldens of `internal/quire/testdata` (two FRs,
  two NFRs — one with a threshold stated in the code, one unknown with
  acceptance criteria —, two indexes).

## What the schema requires of an NFR

- Frontmatter: `id`, `title`, `type: NFR`; `quality_attribute` from
  the same twelve values as `internal/quality`; extra keys admitted
  (`additionalProperties: true`), so the `codetospec_*` keys pass.
- Body (skeleton contract): `## Statement`, `## Measurement and
  Evaluation` with a table headed exactly `Metric | Target | Threshold |
  Method` and at least one row, and `## Verification`; `## Acceptance
  Criteria` (ids `NFR-NNN-AC-N`) and `## Dependencies` optional.
- `MeasurementRow.json`: every cell is `NonEmptyText`. An unknown target
  therefore cannot be an empty cell: it is written as text naming the
  question that asks for it.

## Commands

```sh
UPDATE_GOLDEN=1 go test ./internal/quire
uv run --script validate.py internal/quire/testdata
```

`validate.py` (next to the schema files) validates each frontmatter with
`jsonschema` (Draft 2020-12) against the schema of its `type`, checks the
required sections of FRs and NFRs, the measurement table's header and that
every cell of every row is non-empty, and that each NFR acceptance
criterion id starts with its document id.

## Result

`6 documents, 0 invalid`.

## Limits

- Frontmatter and section shape only, as for the FR check of
  `quire-export-2026-10-26.md`: the record schema `NFR.json` also
  requires `relationships` and `provenance`, which Quire derives when it
  ingests a document; the FR export omits them too.
- Not yet run on a real graph: no existing output has `--quality`
  attributes on technical rules (see
  `quality-business-rules-2026-11-01.md` for the classification run).
