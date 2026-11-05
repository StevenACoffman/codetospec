#!/usr/bin/env python3
"""remapcheck.py: the measurements a remap owes (TODO.md, "Map Input").

Usage: uv run scripts/remapcheck.py RUN_A RUN_B [NAME=BASELINE_DIR ...]

RUN_A and RUN_B are two outputs of the same source, mapped with the same
prompt and model (scripts/remap.sh writes pkg twice). Each BASELINE_DIR is
an earlier output of that source to compare candidate counts with, e.g.
before-remap=~/spec-graphs.before-remap-2026-11-02/pkg. Reads the map caches
(.codetospec/map/*.json) and, through `bin/codetospec diff`, the rules;
writes a markdown report on stdout, never touches an output.

1. Run-to-run variance: per chunk present in both runs, the difference in
   rule candidates; the whole run's totals by nature; the rules diff reports
   between the runs (what a rerun alone changes).
2. Candidates by nature and chunk size, each run against each baseline: the
   technical count the 2026-11-04 map-prompt fix should restore (786 on pkg
   before the remap that lost them), and the small-chunk inflation the
   ablation saw (+29 % under 100 lines).
"""

import collections
import json
import os
import statistics
import subprocess
import sys

# Line windows (files without a grammar) share this many lines with the
# next window (fallbackOverlap, internal/sitter/chunk.go).
WINDOW_OVERLAP = 20
SIZES = (("< 40", 0, 40), ("40-99", 40, 100), (">= 100", 100, 10**9))
NATURES = ("business", "technical", "presentation")


def load(out):
    """chunk id -> (lines, Counter of natures), failed and stale chunks left out.

    The map cache keeps the output of a chunk the code no longer has (its
    file changed, so its content hash did). The current chunks overlap only
    as adjacent line windows do, by WINDOW_OVERLAP lines, and a chunk that
    replaced another was written after it: an entry otherwise overlapping a
    newer one of the same file is stale. A stale entry whose span no current
    chunk overlaps (code removed) is kept: measured, 3 of 89 on pkg.
    """
    d = os.path.join(os.path.expanduser(out), ".codetospec", "map")
    entries = []
    for f in sorted(os.listdir(d)):
        if not f.endswith(".json"):
            continue
        path = os.path.join(d, f)
        with open(path) as fh:
            o = json.load(fh)
        entries.append((os.path.getmtime(path), f[:-5], o))
    newest = collections.defaultdict(list)  # file -> spans, newest first
    chunks = {}
    for _, cid, o in sorted(entries, key=lambda e: (-e[0], e[1])):
        start, end = o["start_line"], o["end_line"]
        spans = newest[o["path"]]
        stale = any(overlaps((start, end), span) for span in spans)
        spans.append((start, end))
        if stale or o.get("failed"):
            continue
        chunks[cid] = (
            end - start + 1,
            collections.Counter(r.get("nature") for r in o["rules"]),
        )
    return chunks


def overlaps(a, b):
    """Spans a and b share a line, other than as adjacent windows do."""
    if a[0] > b[0]:
        a, b = b, a
    shared = min(a[1], b[1]) - b[0] + 1
    return shared > 0 and not (shared == WINDOW_OVERLAP and b[1] > a[1])


def size_of(lines):
    return next(name for name, lo, hi in SIZES if lo <= lines < hi)


def totals(chunks, ids=None):
    """(nature -> count, size -> [chunks, rules])"""
    by_nature, by_size = collections.Counter(), collections.defaultdict(lambda: [0, 0])
    for cid, (lines, natures) in chunks.items():
        if ids is not None and cid not in ids:
            continue
        by_nature.update(natures)
        s = by_size[size_of(lines)]
        s[0] += 1
        s[1] += sum(natures.values())
    return by_nature, by_size


def diff_counts(old, new):
    binary = os.path.join(os.path.dirname(__file__), "..", "bin", "codetospec")
    try:
        out = subprocess.run(
            [
                binary,
                "diff",
                "--old",
                os.path.expanduser(old),
                "--new",
                os.path.expanduser(new),
                "--format",
                "json",
            ],
            capture_output=True,
            text=True,
            check=True,
        ).stdout
    except (OSError, subprocess.CalledProcessError) as e:
        return f"(diff failed: {e})"
    kinds = collections.Counter(c["kind"] for c in json.loads(out).get("changes", []))
    return ", ".join(f"{k} {kinds[k]}" for k in sorted(kinds)) or "no change"


def main(argv):
    if len(argv) < 3:
        sys.exit(__doc__)
    a_dir, b_dir = argv[1], argv[2]
    baselines = [arg.split("=", 1) for arg in argv[3:]]
    a, b = load(a_dir), load(b_dir)
    common = sorted(set(a) & set(b))
    print(
        f"# Remap check\n\nRuns: A `{a_dir}` ({len(a)} chunks), B `{b_dir}` ({len(b)} chunks); "
        f"{len(common)} chunks in both.\n"
    )

    print("## 1. Run-to-run variance (A vs B)\n")
    deltas = [sum(a[c][1].values()) - sum(b[c][1].values()) for c in common]
    changed = sum(1 for d in deltas if d)
    print(
        f"- chunks whose candidate count differs: {changed} of {len(common)}"
        f" ({100 * changed / max(len(common), 1):.0f} %)"
    )
    if deltas:
        print(
            f"- per-chunk difference: mean |d| {statistics.mean(abs(d) for d in deltas):.2f},"
            f" sd {statistics.pstdev(deltas):.2f}, max |d| {max(abs(d) for d in deltas)}"
        )
    ta, _ = totals(a, set(common))
    tb, _ = totals(b, set(common))
    print("\n| nature | A | B | B − A |\n|---|---:|---:|---:|")
    for n in NATURES:
        print(f"| {n} | {ta[n]} | {tb[n]} | {tb[n] - ta[n]:+d} |")
    print(
        f"| all | {sum(ta.values())} | {sum(tb.values())} | "
        f"{sum(tb.values()) - sum(ta.values()):+d} |"
    )
    print(f"\nRules, `codetospec diff --old A --new B`: {diff_counts(a_dir, b_dir)}\n")

    print("## 2. Candidates by nature and chunk size\n")
    runs = [("A", a), ("B", b)] + [(name, load(d)) for name, d in baselines]
    print(
        "| run | chunks | "
        + " | ".join(NATURES)
        + " | all |\n|---|---:|"
        + "---:|" * (len(NATURES) + 1)
    )
    for name, chunks in runs:
        t, _ = totals(chunks)
        print(
            f"| {name} | {len(chunks)} | "
            + " | ".join(str(t[n]) for n in NATURES)
            + f" | {sum(t.values())} |"
        )
    print(
        "\n| run | "
        + " | ".join(f"{s} lines: chunks / rules" for s, _, _ in SIZES)
        + " |\n|---|"
        + "---|" * len(SIZES)
    )
    for name, chunks in runs:
        _, s = totals(chunks)
        print(
            f"| {name} | "
            + " | ".join(f"{s[n][0]} / {s[n][1]}" for n, _, _ in SIZES)
            + " |"
        )
    print(
        "\nRead: technical near the baseline that predates the loss (786 on pkg); "
        "small-chunk rules not far above it; A vs B differences are the noise floor "
        "for every other comparison."
    )


if __name__ == "__main__":
    main(sys.argv)
