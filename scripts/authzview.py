#!/usr/bin/env python3
"""authzview.py: render the authorization rules of codetospec graphs as one
self-contained HTML page (no network, no dependencies).

Usage: uv run scripts/authzview.py [GRAPH_DIR ...] [-o OUT.html] [--fresh]
  default graphs: every ~/spec-graphs/<service> holding a webapp extractor
  facts file; default output: ~/spec-graphs/authz.html
  --fresh: build and re-run the webapp extractor for each service (seconds,
  no LLM) instead of reading the facts of the last scan, so the page shows
  the current extractor; the scan outputs are not modified.

It reads, per service:
  .codetospec/webapp-extractor.facts.json  mechanical rules (authz, rate
                                           limits, open access, policies,
                                           tables) and GraphQL operations
                                           (route facts)
  .codetospec/go-routes.facts.json         net/http routes (the Go
                                           extractor's), when present: the
                                           handlers' paths are entry points
  graph.json + nodes/rules/*.md            LLM rules linked to an operation
                                           whose text is about access
The page stays local: it maps which operation needs which check, and
("Header contracts") which service sends, forwards and requires each secret
header, joined across services by the header and the secret's store name.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

FACTS = ".codetospec/webapp-extractor.facts.json"
GO_ROUTES = ".codetospec/go-routes.facts.json"

# Check categories, read from the extractor's fixed EARS wording (en and fr).
CATEGORIES = [
    ("open", "Open"),
    ("admin", "Service admin"),
    ("login", "Logged in"),
    ("self", "Self"),
    ("coach", "Coach"),
    ("permission", "Permission"),
    ("checker", "Checker"),
    ("secret", "Secret / token"),
    ("policy", "Policy"),
    ("table", "Table"),
    ("ratelimit", "Rate limit"),
    ("condition", "Code condition"),
    ("visibility", "Visibility"),
    ("checked", "Checked by"),
    ("unchecked", "Unchecked"),
    ("served", "Served only when"),
]

PATTERNS = [
    ("admin", re.compile(r"service-admin")),
    ("login", re.compile(r"logged in|connecté")),
    (
        "self",
        re.compile(
            r"caller is (?:not )?the user|l'appelant (?:n')?est (?:pas )?l'utilisateur"
        ),
    ),
    ("coach", re.compile(r"coached by the caller|coach de l'utilisateur")),
    ("permission", re.compile(r"permission `([^`]+)`")),
    ("ratelimit", re.compile(r"rate limit|limite de débit")),
    # a secret header, a shared token, a signed message (authenticators)
    ("secret", re.compile(r'"X-Ka-[\w-]+"|\w*[Tt]oken\b|ValidatePushMsg|[Ss]ecret')),
]
CHECKER = re.compile(r"`([A-Za-z_][\w.]*)\(")
ACCESS_WORDS = re.compile(
    r"unauthori[sz]ed|forbidden|not allowed|permission|authori[sz]|logged[- ]in|"
    r"service[- ]admin|access (?:control|denied)|non autorisé|autorisation",
    re.IGNORECASE,
)


def classify(requirement):
    """Return (categories, permissions, checkers, outcome) of one rule."""
    if re.match(
        r"(The system shall allow any caller|Le systeme doit autoriser)", requirement
    ):
        return ["open"], [], [], "allow"
    if re.match(r"(The system shall serve|Le systeme doit servir)", requirement):
        return ["served"], [], [], "registered only under a condition"
    if re.match(
        r"(The system shall limit `[^`]+` to \d|Le systeme doit limiter `[^`]+` à \d)",
        requirement,
    ):
        return ["ratelimit", "table"], [], [], "limit per operation"
    if re.match(r"(The system shall limit|Le systeme doit limiter)", requirement):
        return ["visibility"], [], CHECKER.findall(requirement)[:1], "filtered"
    if re.match(
        r"(The system shall (hold|map)|Le systeme doit (tenir|associer))", requirement
    ):
        return ["table"], [], [], "table read by a check"
    if re.match(
        r"(The system shall accept a request|Le systeme doit accepter une requête)",
        requirement,
    ):
        cats = ["policy"] + [n for n, pat in PATTERNS if pat.search(requirement)]
        perms = dict(PATTERNS)["permission"].findall(requirement)
        return cats, perms, CHECKER.findall(requirement), "accept only when …"
    if re.match(
        r"(The system does not check access|Le systeme ne vérifie pas encore)",
        requirement,
    ):
        return ["unchecked"], [], [], "⚠ no permission check (acl.TODO)"
    if re.match(
        r"(The system shall check access|Le systeme doit vérifier l'accès)", requirement
    ):
        name = re.findall(r"`([^`]+)`\.?$", requirement)
        return ["checked"], [], name, "checked here"
    cats, perms = [], []
    condition = re.split(r",\s*(?:THEN|ALORS)\s", requirement, maxsplit=1)[0]
    for name, pat in PATTERNS:
        # what is checked is in the condition; the operation is named after
        found = pat.findall(condition if name == "secret" else requirement)
        if found:
            cats.append(name)
            if name == "permission":
                perms.extend(found)
    checkers = [
        c
        for c in CHECKER.findall(condition)
        if c.split(".")[0] not in ("ratelimit", "time")
    ]
    if checkers:
        cats.append("checker")
    if (
        not cats
    ):  # a verbatim condition: ownership (`x.KAID != user.Kaid`), a flag, a nil test
        cats.append("condition")
    outcome = (
        "no data"
        if re.search(r"return no data|aucune donnée", requirement)
        else "reject"
    )
    if re.search(r"reject that `|rejeter cet élément", requirement):
        outcome = "reject the item"
    if re.search(
        r"the caller themselves \(never true\)|lui-même \(jamais vrai\)", requirement
    ):
        outcome += " · ⚠ self-check never denies"
    err = re.search(r"with the `(\w+)` error|avec l'erreur `(\w+)`", requirement)
    if err:
        outcome += " · " + (err.group(1) or err.group(2))
    return cats, perms, checkers, outcome


def operation_of(title):
    """'Access control: Mutation.addCoach (via _check) (2)' -> 'Mutation.addCoach'."""
    name = title.split(":", 1)[-1].strip()
    return re.sub(r"(\s*\((\d+|via \w+)\))+$", "", name)


def operations_of(title):
    """The operations a rule is about: a handler's rule names its paths
    ('/graphql/, /tasks/graphql/'), any other one operation."""
    name = operation_of(title)
    return name.split(", ") if name.startswith("/") else [name]


def endpoint_id(name):
    """The graph node of an operation: endpoint.mutation-addcoach, or
    endpoint.safelist-add for the path /safelist/add."""
    words = name if name.startswith("/") else name.replace(".", " ", 1)
    return "endpoint." + re.sub(r"[^a-z0-9]+", "-", words.lower()).strip("-")


def requirement_of(markdown):
    m = re.search(
        r"\*\*(?:Requirement \(EARS\)|Exigence \(EARS\))\*\*\s*:\s*(.*)", markdown
    )
    return m.group(1).strip() if m else ""


def fresh_facts(service, webapp, extractor):
    """Run the webapp extractor on services/<service>; its facts."""
    out = subprocess.run(
        [str(extractor), "-root", str(webapp / "services" / service), "-lang", "en"],
        check=True,
        capture_output=True,
        text=True,
    )
    return json.loads(out.stdout)["facts"]


def build_extractor(repo, name="webapp"):
    """Build extractors/<name> into a temporary binary."""
    binary = Path(tempfile.mkdtemp(prefix="authzview-")) / (name + "-extract")
    subprocess.run(
        ["go", "build", "-o", str(binary), "."],
        cwd=repo / "extractors" / name,
        check=True,
    )
    return binary


def fresh_routes(service, webapp, extractor):
    """Run the Go extractor on services/<service> (test helpers' mock servers
    skipped, as scanwebapp does); its facts, or None when packages fail to load."""
    out = subprocess.run(
        [str(extractor), "-root", ".", "-skip-dirs", "testutil"],
        cwd=webapp / "services" / service,
        capture_output=True,
        text=True,
        check=False,
    )
    if out.returncode != 0:
        return None
    return json.loads(out.stdout)["facts"] or []


def contracts_of(facts_by_service):
    """header → its contract across services: the secret's store name, and the
    rules that send, forward or require it (contract attrs), plus the other
    rules deciding on it (they quote the header). A rule of webapp's shared
    code, inherited by several services, is listed once with their names."""
    contracts = {}
    for svc, facts in sorted(facts_by_service.items()):
        for f in facts:
            a = f["attrs"]
            if f["kind"] != "rule":
                continue
            src = f["source"]
            where = ("@" + src["root"] + "/" if src.get("root") else "") + src["path"]
            entry = {
                "source": f"{where}:{src['lines']}",
                "requirement": a["requirement"],
            }
            if a.get("contract"):
                for header in a["header"].split(","):
                    c = contracts.setdefault(header, {"secrets": [], "roles": {}})
                    if a.get("secret") and a["secret"] not in c["secrets"]:
                        c["secrets"].append(a["secret"])
                    add_site(c["roles"].setdefault(a["contract"], []), entry, svc)
    for svc, facts in sorted(facts_by_service.items()):
        for f in facts:
            a = f["attrs"]
            if f["kind"] != "rule" or a.get("contract"):
                continue
            src = f["source"]
            where = ("@" + src["root"] + "/" if src.get("root") else "") + src["path"]
            for header, c in contracts.items():
                if (
                    f'"{header}"' in a["requirement"]
                    or f"`{header}`" in a["requirement"]
                ):
                    entry = {
                        "source": f"{where}:{src['lines']}",
                        "requirement": a["requirement"],
                    }
                    add_site(c["roles"].setdefault("decides", []), entry, svc)
    return contracts


def add_site(sites, entry, svc):
    """Add a rule to a contract role, or the service to the same rule."""
    for s in sites:
        if s["source"] == entry["source"] and s["requirement"] == entry["requirement"]:
            if svc not in s["services"]:
                s["services"].append(svc)
            return
    sites.append({**entry, "services": [svc]})


def saved_routes(graph_dir):
    """The Go extractor's facts of the last scan; [] when absent."""
    routes = graph_dir / GO_ROUTES
    if not routes.exists():
        return []
    return json.loads(routes.read_text())["facts"] or []


def admin_callers(facts_by_service):
    """(callee service, operation) → the call sites, in other services, that
    call it as a service-admin query (dependency facts with as=service-admin)."""
    callers = {}
    for caller, facts in sorted(facts_by_service.items()):
        for f in facts:
            a = f["attrs"]
            if f["kind"] == "dependency" and a.get("as") == "service-admin":
                site = f"{caller} {f['source']['path']}:{f['source']['lines']}"
                callers.setdefault((a["service"], a["operation"]), []).append(site)
    return callers


def roles_of(facts):
    """The role rules of a service's facts (the admin service registers them):
    role name → its capabilities (Go names) and citation."""
    roles = {}
    for f in facts:
        a = f["attrs"]
        if f["kind"] == "rule" and a.get("slug", "").startswith("role-"):
            caps = [c for c in a.get("capabilities", "").split(",") if c]
            roles[a["role"]] = {
                "caps": caps,
                "source": f["source"]["path"] + ":" + f["source"]["lines"],
            }
    return roles


def site_admin_note(webapp):
    """Where admin grants site admins every capability (IsUserAdmin in
    services/admin/permissions/scopes.go), as "path:line"; "" when not found."""
    scopes = webapp / "services" / "admin" / "permissions" / "scopes.go"
    if not scopes.exists():
        return ""
    for n, line in enumerate(scopes.read_text().split("\n"), 1):
        if "IsUserAdmin(" in line and "if " in line:
            return f"services/admin/permissions/scopes.go:{n}"
    return ""


def load_service(graph_dir, facts, routes, callers):
    """A scanned service's operations: those of its facts, and the LLM rules of
    its graph linked to them."""
    ops = build_ops(graph_dir.name, facts, routes, callers)
    attach_llm_rules(ops, graph_dir)
    return ops


def build_ops(service, facts, routes, callers):
    """The operations of a service and their mechanical rules, from its facts
    (webapp extractor) and routes (Go extractor); callers as admin_callers
    returns. Entry points are its GraphQL fields and net/http handlers."""
    ops = {}

    def op(name):
        return ops.setdefault(
            name, {"name": name, "root": False, "domain": "", "rules": [], "llm": []}
        )

    for f in facts:
        if f["kind"] == "route" and f.get("origin") == "webapp":
            o = op(f["attrs"]["method"].title() + "." + f["attrs"]["path"])
            o["root"] = True
            o["endpoint"] = f["id"]
    for f in routes:
        path = f["attrs"].get("path", "")
        if f["kind"] == "route" and f["attrs"].get("handler_path") and path:
            o = op(path)
            o["root"] = True
            o["endpoint"] = f["id"]
    for f in facts:
        if f["kind"] != "rule" or f["attrs"].get("slug", "").startswith("role-"):
            continue
        a = f["attrs"]
        cats, perms, checkers, outcome = classify(a["requirement"])
        for name in operations_of(a["title"]):
            o = op(name)
            o["domain"] = o["domain"] or a.get("domain", "")
            o["rules"].append(
                {
                    "requirement": a["requirement"],
                    "source": f["source"]["path"] + ":" + f["source"]["lines"],
                    "cats": cats,
                    "perms": perms,
                    "checkers": checkers,
                    "outcome": outcome,
                }
            )
    for o in ops.values():
        o["cats"] = sorted(
            {c for r in o["rules"] for c in r["cats"]},
            key=[c for c, _ in CATEGORIES].index,
        )
        o["perms"] = sorted({p for r in o["rules"] for p in r["perms"]})
        o["checkers"] = sorted({c for r in o["rules"] for c in r["checkers"]})
        o["adminCallers"] = callers.get((service, o["name"]), [])
    return sorted(ops.values(), key=lambda o: (o["domain"], o["name"]))


def attach_llm_rules(ops, graph_dir):
    """Add to the entry points of ops the LLM rules of graph_dir (a codetospec
    graph) linked to their endpoint whose text is about access."""
    endpoint_op = {endpoint_id(o["name"]): o for o in ops if o["root"]}
    graph = json.loads((graph_dir / "graph.json").read_text())["nodes"]
    for node in graph.values():
        if node.get("type") != "rule":
            continue
        targets = [
            e["to"]
            for e in node.get("edges", [])
            if e["type"] == "exposed_by" and e["to"] in endpoint_op
        ]
        if not targets:
            continue
        md = (graph_dir / node["file"]).read_text()
        if "extracted_by:" in md:
            continue
        req = requirement_of(md)
        if not ACCESS_WORDS.search(req):
            continue
        src = re.search(r'  - path: (.*)\n    lines: "(.*)"', md)
        for t in targets:
            endpoint_op[t]["llm"].append(
                {
                    "requirement": req,
                    "source": f"{src.group(1)}:{src.group(2)}" if src else "",
                }
            )


def render_page(data, roles, contracts, site_admin):
    """The self-contained page: data (service → operations), the role grants,
    the header contracts and the site-admin note."""

    def js(value):
        return json.dumps(value).replace("</", "<\\/")

    return (
        PAGE.replace("__DATA__", js(data))
        .replace("__CATS__", json.dumps(CATEGORIES))
        .replace("__ROLES__", js(roles))
        .replace("__CONTRACTS__", js(contracts))
        .replace("__SITEADMIN__", json.dumps(site_admin))
    )


PAGE = """<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorization Map</title>
<style>
:root { --bg:#fbfbfa; --fg:#1d1d1b; --muted:#6b6b66; --line:#e3e2dd; --card:#ffffff;
  --accent:#2f5d8a; --ok:#2e7d4f; --warn:#b4552d; --chip:#eef2f7; --open:#7a5a00; }
@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) {
  --bg:#161615; --fg:#ecebe6; --muted:#a3a29b; --line:#33322f; --card:#1e1e1c;
  --accent:#8fb4dc; --ok:#6cc495; --warn:#e8946f; --chip:#26303b; --open:#e2c25a; } }
:root[data-theme="dark"] { --bg:#161615; --fg:#ecebe6; --muted:#a3a29b; --line:#33322f; --card:#1e1e1c;
  --accent:#8fb4dc; --ok:#6cc495; --warn:#e8946f; --chip:#26303b; --open:#e2c25a; }
* { box-sizing:border-box } body { margin:0; background:var(--bg); color:var(--fg);
  font:14px/1.45 system-ui,-apple-system,"Segoe UI",sans-serif; }
main { max-width:1280px; margin:0 auto; padding:20px 16px 60px }
h1 { font-size:20px; margin:0 0 4px } .sub { color:var(--muted); margin:0 0 16px }
.tabs { display:flex; flex-wrap:wrap; gap:6px; margin:12px 0 }
.tabs button, .views button { border:1px solid var(--line); background:var(--card); color:var(--fg);
  padding:6px 12px; border-radius:6px; cursor:pointer; font:inherit }
.tabs button[aria-pressed="true"], .views button[aria-pressed="true"] { border-color:var(--accent); color:var(--accent); font-weight:600 }
.stats { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:8px; margin:8px 0 14px }
.stat { background:var(--card); border:1px solid var(--line); border-radius:8px; padding:10px 12px }
.stat b { display:block; font-size:20px } .stat span { color:var(--muted); font-size:12px }
.controls { display:flex; flex-wrap:wrap; gap:10px; align-items:center; margin:10px 0 }
.controls input[type=search] { flex:1 1 220px; padding:7px 10px; border:1px solid var(--line);
  border-radius:6px; background:var(--card); color:var(--fg); font:inherit }
.wrap { overflow-x:auto; border:1px solid var(--line); border-radius:8px; background:var(--card) }
table { border-collapse:collapse; width:100%; min-width:860px }
th, td { padding:6px 8px; border-bottom:1px solid var(--line); text-align:left; vertical-align:top }
th { position:sticky; top:0; background:var(--card); font-size:12px; color:var(--muted); font-weight:600 }
td.c { text-align:center; width:74px } .dot { color:var(--accent); font-size:16px }
.open .dot { color:var(--open) } tr.op { cursor:pointer } tr.op:hover { background:var(--chip) }
th.sort { cursor:pointer; user-select:none } th.sort:hover { color:var(--fg) }
.name { font-family:ui-monospace,Menlo,monospace; font-size:13px } .dom { color:var(--muted); font-size:12px }
.gap { color:var(--warn); font-weight:600; font-size:12px } .chip { display:inline-block; background:var(--chip);
  border-radius:4px; padding:0 6px; margin:1px 2px; font-family:ui-monospace,Menlo,monospace; font-size:12px }
tr.detail td { background:var(--bg); } .rule { margin:6px 0 10px } .rule .src, .llm .src { color:var(--muted);
  font-family:ui-monospace,Menlo,monospace; font-size:12px } .outcome { color:var(--muted); font-size:12px }
.llm { border-left:3px solid var(--line); padding-left:8px; margin:6px 0 } .llmh { color:var(--muted); font-size:12px; margin-top:8px }
.legend { color:var(--muted); font-size:12px; margin:10px 0 } .perm h3 { margin:14px 0 4px; font-size:14px }
</style></head><body><main>
<h1>Authorization map</h1>
<p class="sub">Mechanical access rules found by the webapp extractor in each resolver, per GraphQL
operation. A dot means the check takes part in deciding access; one operation can have several rules.
LLM-described rules about access are listed under each operation, unverified.</p>
<div class="tabs" id="tabs"></div>
<div class="stats" id="stats"></div>
<div class="views" id="views"></div>
<div class="controls"><input type="search" id="q" placeholder="Filter operations, permissions, checkers…">
<label><input type="checkbox" id="mut"> Mutations only</label>
<label><input type="checkbox" id="gaps"> No mechanical rule only</label></div>
<div id="view"></div>
<p class="legend">“No mechanical rule” means the extractor recognised no guard; the check may live in a
shape it does not read, or the operation may be public by design. Service admin marks a bypass for
service-admin queries. A capability is held through a role or a direct grant, each on the scope
it was granted on (a user, a country…); the check then matches that scope (AnyScope accepts any,
GlobalScope only a global one, UserScope only one covering the target). So "granted by roles" says
who may hold a capability, not on which data, and does not list direct grants.</p>
<p class="legend" id="siteadmin"></p>
</main>
<script id="data" type="application/json">__DATA__</script>
<script>
const DATA = JSON.parse(document.getElementById("data").textContent);
const CATS = __CATS__;
const ROLES = __ROLES__, SITE_ADMIN = __SITEADMIN__, CONTRACTS = __CONTRACTS__;
const CAP_ROLES = {}; // capability → roles granting it
Object.entries(ROLES).forEach(([r, v]) => v.caps.forEach((c) => (CAP_ROLES[c] ||= []).push(r)));
const rolesOf = (cap) => (CAP_ROLES[cap] || []).sort();
let svc = Object.keys(DATA)[0], view = "matrix";
const $ = (id) => document.getElementById(id);
const esc = (s) => s.replace(/[&<>"]/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[c]));
const code = (s) => esc(s).replace(/`([^`]+)`/g, '<span class="chip">$1</span>');
function tabs() {
  $("tabs").innerHTML = Object.keys(DATA).map((s) =>
    `<button aria-pressed="${s === svc}" data-s="${s}">${s}</button>`).join("");
  $("views").innerHTML = [["matrix","Operations × checks"],["perms","By permission"],["roles","By role"],["gaps","Gaps"],["contracts","Header contracts"]]
    .map(([v, l]) => `<button aria-pressed="${v === view}" data-v="${v}">${l}</button>`).join(" ");
}
function filtered() {
  const q = $("q").value.toLowerCase(), mut = $("mut").checked, gaps = $("gaps").checked || view === "gaps";
  return DATA[svc].filter((o) => (!mut || o.name.startsWith("Mutation.")) && (!gaps || (o.root && !o.rules.length))
    && (!q || (o.name + " " + o.domain + " " + o.perms.join(" ") + " " + o.checkers.join(" ")).toLowerCase().includes(q)));
}
function stats() {
  const ops = DATA[svc], root = ops.filter((o) => o.root), guarded = root.filter((o) => o.rules.length);
  const rules = ops.reduce((n, o) => n + o.rules.length, 0);
  const perms = new Set(ops.flatMap((o) => o.perms)), open = ops.filter((o) => o.cats.includes("open")).length;
  $("stats").innerHTML = [[root.length, "entry points"], [guarded.length + " (" + Math.round(100 * guarded.length / Math.max(root.length, 1)) + "%)", "with a mechanical rule"],
    [rules, "mechanical rules"], [perms.size, "permissions checked"], [open, "open-access operations"]]
    .map(([b, s]) => `<div class="stat"><b>${b}</b><span>${s}</span></div>`).join("");
}
function detail(o) {
  const rules = o.rules.map((r) => `<div class="rule">${code(r.requirement)}<div class="src">${esc(r.source)} · <span class="outcome">${esc(r.outcome)}</span></div></div>`).join("");
  const llm = o.llm.length ? `<div class="llmh">LLM-described (${o.llm.length}, unverified)</div>` +
    o.llm.map((r) => `<div class="llm">${code(r.requirement)}<div class="src">${esc(r.source)}</div></div>`).join("") : "";
  const grants = o.perms.map((p) => `<div class="src">Roles granting <span class="chip">${esc(p)}</span>: ${
    rolesOf(p).map((r) => `<span class="chip">${esc(r)}</span>`).join(" ") || "none (only a direct grant or a site admin)"}</div>`).join("");
  const admins = o.adminCallers.length ? `<div class="src">Called as a service-admin query by: ${
    o.adminCallers.map((c) => `<span class="chip">${esc(c)}</span>`).join(" ")}</div>` : "";
  return rules || llm ? rules + grants + admins + llm : '<span class="gap">No rule describes access to this operation.</span>' + admins;
}
function roles() {
  const ops = filtered();
  const names = Object.keys(ROLES).sort();
  if (!names.length) { $("view").innerHTML = "<p>No role rules: scan the admin service too.</p>"; return; }
  $("view").innerHTML = names.map((r) => {
    const caps = ROLES[r].caps;
    const reach = ops.filter((o) => o.perms.some((p) => caps.includes(p)));
    return `<div class="perm"><h3><span class="chip">${esc(r)}</span> · ${caps.length} capabilities · ${reach.length} operations here</h3>
      <div class="src">${esc(ROLES[r].source)}</div>${caps.map((c) => `<span class="chip">${esc(c)}</span>`).join(" ")}
      <div>${reach.map((o) => `<span class="chip">${esc(o.name)}</span>`).join(" ")}</div></div>`;
  }).join("");
}
let sortCol = "", sortDir = 1; // "": the page's order (domain, operation)
function sortKey(o, col) {
  if (col === "op") return o.name.toLowerCase();
  if (col === "extra") return o.perms.concat(o.checkers).join(" ").toLowerCase();
  return o.cats.includes(col.slice(4)) ? "0" : "1"; // "cat:<c>": marked rows first
}
function sorted(list) {
  if (!sortCol) return list;
  return list.map((o, i) => [o, i]).sort(([a, i], [b, j]) => {
    const ka = sortKey(a, sortCol), kb = sortKey(b, sortCol);
    if (sortCol === "extra" && (ka === "") !== (kb === "")) return ka === "" ? 1 : -1; // empty last
    return (ka < kb ? -1 : ka > kb ? 1 : 0) * sortDir || i - j; // ties keep the page's order
  }).map(([o]) => o);
}
function th(col, label) {
  const on = sortCol === col, arrow = on ? (sortDir > 0 ? " ▲" : " ▼") : "";
  return `<th class="sort" data-col="${col}" aria-sort="${on ? (sortDir > 0 ? "ascending" : "descending") : "none"}">${label}${arrow}</th>`;
}
function matrix() {
  const list = sorted(filtered());
  const rows = list.map((o, i) => {
    const cells = CATS.map(([c]) => `<td class="c ${c}">${o.cats.includes(c) ? '<span class="dot">●</span>' : ""}</td>`).join("");
    const extra = o.perms.map((p) => `<span class="chip">${esc(p)}</span>`).join("") + o.checkers.map((p) => `<span class="chip">${esc(p)}()</span>`).join("");
    const gap = o.root && !o.rules.length ? ' <span class="gap">no mechanical rule</span>' : "";
    return `<tr class="op" data-i="${i}"><td><div class="name">${esc(o.name)}${gap}</div><div class="dom">${esc(o.domain || "")}</div></td>${cells}<td>${extra}</td></tr>`;
  }).join("");
  $("view").innerHTML = `<div class="wrap"><table><thead><tr>${th("op", "Operation")}${CATS.map(([c, l]) => th("cat:" + c, l)).join("")}${th("extra", "Permissions · checkers")}</tr></thead><tbody>${rows}</tbody></table></div>`;
  $("view").querySelectorAll("th.sort").forEach((h) => h.addEventListener("click", () => {
    if (sortCol === h.dataset.col) sortDir = -sortDir; else { sortCol = h.dataset.col; sortDir = 1; }
    matrix(); // same filters and checkboxes: filtered() reads them
  }));
  $("view").querySelectorAll("tr.op").forEach((tr) => tr.addEventListener("click", () => {
    const next = tr.nextElementSibling;
    if (next && next.classList.contains("detail")) { next.remove(); return; }
    tr.insertAdjacentHTML("afterend", `<tr class="detail"><td colspan="${CATS.length + 2}">${detail(list[+tr.dataset.i])}</td></tr>`);
  }));
}
function perms() {
  const by = {};
  filtered().forEach((o) => { o.perms.forEach((p) => (by[p] ||= []).push(o)); o.checkers.forEach((c) => (by[c + "()"] ||= []).push(o)); });
  $("view").innerHTML = Object.keys(by).sort().map((p) => `<div class="perm"><h3><span class="chip">${esc(p)}</span> · ${by[p].length}</h3><div class="src">granted by roles: ${rolesOf(p).join(", ") || "none (only a direct grant or a site admin)"}</div>${by[p].map((o) => `<span class="chip">${esc(o.name)}</span>`).join(" ")}</div>`).join("") || "<p>No permission in view.</p>";
}
const ROLE_LABELS = [["sends", "Sent by"], ["forwards", "Forwarded by"], ["requires", "Required by"], ["decides", "Decisions using it"]];
function contracts() {
  const headers = Object.keys(CONTRACTS).sort();
  if (!headers.length) { $("view").innerHTML = "<p>No header contract: rescan with the current extractor.</p>"; return; }
  $("view").innerHTML = headers.map((h) => {
    const c = CONTRACTS[h];
    const roles = ROLE_LABELS.filter(([r]) => c.roles[r]).map(([r, label]) =>
      `<div class="llmh">${label} (${c.roles[r].length})</div>` + c.roles[r].map((site) =>
        `<div class="rule">${code(site.requirement)}<div class="src">${esc(site.source)} · ${
          site.services.map((s) => `<span class="chip">${esc(s)}</span>`).join(" ")}</div></div>`).join("")).join("");
    const secrets = c.secrets.length ? " · secret " + c.secrets.map((x) => `<span class="chip">${esc(x)}</span>`).join(" ") : "";
    return `<div class="perm"><h3><span class="chip">${esc(h)}</span>${secrets}</h3>${roles}</div>`;
  }).join("");
}
function render() {
  tabs(); stats();
  if (view === "perms") perms(); else if (view === "roles") roles(); else if (view === "contracts") contracts(); else matrix();
  $("siteadmin").textContent = SITE_ADMIN ? `Site admins hold every capability on the global scope, whatever their roles (${SITE_ADMIN}): ` +
    "members of the GCP IAM bindings roles/owner, …/roles/swe_base and …/roles/devadminsuperuser, plus the " +
    "hard-coded emails of permissions/admin.go — always in production (prodIAMClient.AlwaysIncludeFallbackEmails), " +
    "on dev only when IAM cannot be read. Only once their stored " +
    "UserData.HasAnyPermissions flag is true: the calling service skips the admin service otherwise, and a " +
    "periodic sync sets it. Admin status is cached for an hour." : "";
}
$("tabs").addEventListener("click", (e) => { if (e.target.dataset.s) { svc = e.target.dataset.s; render(); } });
$("views").addEventListener("click", (e) => { if (e.target.dataset.v) { view = e.target.dataset.v; render(); } });
["q", "mut", "gaps"].forEach((id) => $(id).addEventListener("input", render));
render();
</script></body></html>
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("graphs", nargs="*", type=Path)
    parser.add_argument(
        "-o", "--out", type=Path, default=Path.home() / "spec-graphs" / "authz.html"
    )
    parser.add_argument(
        "--fresh",
        action="store_true",
        help="re-run the current webapp extractor instead of the last scan's facts",
    )
    parser.add_argument(
        "--webapp",
        type=Path,
        default=Path(os.environ.get("WEBAPP_DIR", Path.home() / "khan" / "webapp")),
    )
    args = parser.parse_args()
    repo = Path(__file__).resolve().parent.parent
    extractor = build_extractor(repo) if args.fresh else None
    go_extractor = build_extractor(repo, "go") if args.fresh else None
    graphs = args.graphs or sorted(
        p.parent.parent for p in (Path.home() / "spec-graphs").glob("*/" + FACTS)
    )
    data, roles, facts_by, routes_by = {}, {}, {}, {}
    for g in graphs:
        if not (g / FACTS).exists():
            print(f"authzview: skip {g}: no {FACTS}", file=sys.stderr)
            continue
        facts = fresh_facts(g.name, args.webapp, extractor) if extractor else None
        if facts is None:
            facts = json.loads((g / FACTS).read_text())["facts"]
        routes = (
            fresh_routes(g.name, args.webapp, go_extractor) if go_extractor else None
        )
        facts_by[g], routes_by[g] = (
            facts,
            routes if routes is not None else saved_routes(g),
        )
        roles.update(roles_of(facts))
    callers = admin_callers({g.name: f for g, f in facts_by.items()})
    for g, facts in facts_by.items():
        ops = load_service(g, facts, routes_by[g], callers)
        if any(o["rules"] or o["root"] for o in ops):
            data[g.name] = ops
    if not data:
        sys.exit("authzview: no graph with webapp extractor facts")
    contracts = contracts_of({g.name: f for g, f in facts_by.items()})
    page = render_page(data, roles, contracts, site_admin_note(args.webapp))
    args.out.write_text(page)
    total = sum(len(o["rules"]) for ops in data.values() for o in ops)
    print(
        f"authzview: {len(data)} services, {total} mechanical rules, {len(roles)} roles → {args.out}",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
