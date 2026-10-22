#!/usr/bin/env python3
"""authzall.py: list every authorization rule of a whole webapp checkout.

Usage: uv run scripts/authzall.py --facts ALL.facts.json [--routes-dir DIR]
                                  [--webapp ~/khan/webapp] -o OUT_DIR

  --facts       the consolidated facts of `webapp-extract -all` (citations
                relative to the checkout, attr in_services on every fact)
  --routes-dir  <service>.facts.json files of the Go extractor (net/http
                routes, so handlers count as entry points); optional
  -o            output directory: authz.html (the authzview page, one tab per
                service plus webapp-shared), authz.csv and authz.md (every
                rule, one line each)

It reuses authzview's model: the same categories, operations, gaps and
header contracts, without a codetospec scan (mechanical rules only).
"""

import argparse
import csv
import json
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import authzview

SHARED = "webapp-shared"  # the tab of shared (pkg/) rules, listed once


def split_by_service(facts):
    """(own, inherited): each service's own facts, and each service's facts
    with the shared ones it inherits (for contracts and admin callers)."""
    own, inherited = {}, {}
    for f in facts:
        services = f["attrs"].get("in_services", "").split(",")
        shared = f["source"]["path"].startswith("pkg/")
        for svc in services:
            if not svc:
                continue
            inherited.setdefault(svc, []).append(f)
            if not shared:
                own.setdefault(svc, []).append(f)
        if shared and f["kind"] == "rule":
            own.setdefault(SHARED, []).append(f)
    return own, inherited


def routed_services(routes_dir):
    """The services with a routes file: listed even with no webapp fact, so
    their net/http entry points count."""
    if routes_dir is None:
        return set()
    return {p.name.removesuffix(".facts.json") for p in routes_dir.glob("*.facts.json")}


def load_routes(routes_dir, service):
    """The Go extractor's routes of a service; [] without them."""
    if routes_dir is None:
        return []
    path = routes_dir / (service + ".facts.json")
    if not path.exists():
        return []
    return json.loads(path.read_text())["facts"] or []


def rule_rows(data, facts):
    """One row per rule of data (service → operations), with the services a
    shared rule is inherited by, then one per role grant (admin's roles)."""
    services_of = {
        f["source"]["path"] + ":" + f["source"]["lines"]: f["attrs"].get(
            "in_services", ""
        )
        for f in facts
        if f["kind"] == "rule"
    }
    for svc, ops in sorted(data.items()):
        for o in ops:
            for r in o["rules"]:
                yield {
                    "service": svc,
                    "operation": o["name"],
                    "entry_point": "yes" if o["root"] else "",
                    "categories": " ".join(r["cats"]),
                    "permissions": " ".join(r["perms"]),
                    "checkers": " ".join(r["checkers"]),
                    "outcome": r["outcome"],
                    "requirement": r["requirement"],
                    "source": r["source"],
                    "in_services": services_of.get(r["source"], svc),
                }
    for f in facts:
        a = f["attrs"]
        if f["kind"] == "rule" and a.get("slug", "").startswith("role-"):
            yield {
                "service": a.get("in_services", ""),
                "operation": "role " + a.get("role", ""),
                "entry_point": "",
                "categories": "role",
                "permissions": " ".join(a.get("capabilities", "").split(",")),
                "checkers": "",
                "outcome": "grants",
                "requirement": a["requirement"],
                "source": f["source"]["path"] + ":" + f["source"]["lines"],
                "in_services": a.get("in_services", ""),
            }


def write_csv(path, rows):
    rows = list(rows)
    with path.open("w", newline="") as out:
        w = csv.DictWriter(out, fieldnames=list(rows[0]) if rows else ["service"])
        w.writeheader()
        w.writerows(rows)
    return rows


def write_markdown(path, rows, gaps):
    lines = ["# Authorization rules of webapp", ""]
    lines.append(
        f"{len(rows)} mechanical rules (a handler serving several paths is listed "
        f"under each), {len({r['service'] for r in rows})} services, "
        f"{len(gaps)} entry points without a rule."
    )
    if gaps:
        lines += ["", "## Entry points without a rule", ""]
        lines += [f"- {g}" for g in gaps]
    service = None
    for r in sorted(rows, key=lambda r: r["service"]):
        if r["service"] != service:
            service = r["service"]
            lines += ["", f"## {service}", ""]
        lines.append(f"- **{r['operation']}** — {r['requirement']} (`{r['source']}`)")
    path.write_text("\n".join(lines) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--facts", type=Path, required=True)
    parser.add_argument("--routes-dir", type=Path)
    parser.add_argument(
        "--webapp",
        type=Path,
        default=Path(os.environ.get("WEBAPP_DIR", Path.home() / "khan" / "webapp")),
    )
    parser.add_argument("-o", "--out", type=Path, required=True)
    args = parser.parse_args()

    facts = json.loads(args.facts.read_text())["facts"] or []
    own, inherited = split_by_service(facts)
    callers = authzview.admin_callers(inherited)
    data = {}
    for svc in sorted(set(own) | routed_services(args.routes_dir)):
        routes = [] if svc == SHARED else load_routes(args.routes_dir, svc)
        ops = authzview.build_ops(svc, own.get(svc, []), routes, callers)
        if any(o["rules"] or o["root"] for o in ops):
            data[svc] = ops
    if not data:
        sys.exit("authzall: no rule in " + str(args.facts))

    args.out.mkdir(parents=True, exist_ok=True)
    page = authzview.render_page(
        data,
        authzview.roles_of(facts),
        authzview.contracts_of(inherited),
        authzview.site_admin_note(args.webapp),
    )
    (args.out / "authz.html").write_text(page)
    rows = write_csv(args.out / "authz.csv", rule_rows(data, facts))
    gaps = [
        f"{svc} {o['name']}"
        for svc, ops in sorted(data.items())
        for o in ops
        if o["root"] and not o["rules"]
    ]
    write_markdown(args.out / "authz.md", rows, gaps)
    print(
        f"authzall: {len(data)} services, {len(rows)} rules, "
        f"{len(gaps)} entry points without a rule → {args.out}",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
