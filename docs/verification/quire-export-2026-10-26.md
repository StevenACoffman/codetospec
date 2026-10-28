# Quire export against the published schemas — 2026-10-26

## Question

Does `codetospec export` write functional requirements that Quire
accepts? No `quire validate` binary is published, so the check runs the
module's own frontmatter schemas and the section rules of its FR
skeleton.

## Revisions

- codetospec: `cf02426` plus the uncommitted change of the same day
  (the export code is unchanged by it).
- Schemas: agent-ix/spec-artifacts-iso 0.3.0 at `9f17d63`
  (`schemas/fr-frontmatter.schema.json`,
  `schemas/index-frontmatter.schema.json`), downloaded beside the script.
- Graphs: `~/spec-graphs/districts-jobs/roster` (source districts-jobs
  `eda9a0cfe`) and `~/spec-graphs/districts` (webapp `1ec3acc9594`), read
  only.

## Commands

```sh
go build -o bin/codetospec ./cmd/codetospec
./bin/codetospec export --out ~/spec-graphs/districts-jobs/roster --to /tmp/qx-roster
./bin/codetospec export --out ~/spec-graphs/districts --to /tmp/qx-districts
uv run --script validate.py /tmp/qx-roster /tmp/qx-districts
```

`validate.py`, next to the two schema files:

```python
# /// script
# dependencies = ["jsonschema", "pyyaml"]
# ///
import json, re, sys, pathlib, yaml, jsonschema
here = pathlib.Path(__file__).parent
fr = json.load(open(here / "fr-frontmatter.schema.json"))
idx = json.load(open(here / "index-frontmatter.schema.json"))
bad = 0
for d in sys.argv[1:]:
    files = sorted(pathlib.Path(d, "functional").glob("*.md"))
    for f in files:
        text = f.read_text()
        m = re.match(r"---\n(.*?)\n---\n", text, re.S)
        front = yaml.safe_load(m.group(1))
        schema = idx if f.name == "index.md" else fr
        try:
            jsonschema.validate(front, schema)
        except jsonschema.ValidationError as e:
            bad += 1; print(f, e.message); continue
        if f.name == "index.md":
            continue
        doc = front["id"]
        for h in ("## Description", "## Acceptance Criteria", "## Dependencies"):
            if "\n" + h + "\n" not in text:
                bad += 1; print(f, "missing", h)
        rows = re.findall(r"^\| (\S+) \| .* \| (\S+) \|$", text, re.M)
        rows = [r for r in rows if r[0] not in ("ID",) and not r[0].startswith("-")]
        if not rows or any(not re.fullmatch(re.escape(doc) + r"-AC-\d+", r[0]) for r in rows):
            bad += 1; print(f, "bad AC rows", rows[:3])
        if text.count("\n| ") != len(rows) + 1:
            bad += 1; print(f, "a table row does not parse (unescaped pipe?)")
    print(d, len(files), "files checked")
print("invalid:", bad)
```

## Results

| Graph     | FRs written | Refuted rules left out | Invalid files |
| --------- | ----------: | ---------------------: | ------------: |
| roster    |         784 |                      0 |             0 |
| districts |       3,556 |                      0 |             0 |

## Limits

- Checks the frontmatter schemas and the required sections and table
  shape only; Quire's own validator may check more (cross-document
  links, its manifest rules).
- Neither graph was crosschecked (their rules have no verdict, or
  `unchecked`: roster 223, districts 771), so no
  rule was left out as refuted.
