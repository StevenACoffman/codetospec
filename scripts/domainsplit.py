#!/usr/bin/env python3
"""domainsplit.py: split a codetospec output's rules into subject matter and
technical concerns, from evidence (DOMAIN_PLAN.md, recommendation 4). No LLM;
reads the output, never writes to it.

Usage: uv run scripts/domainsplit.py GRAPH_DIR SERVICE_SRC_DIR OUT_DIR

Writes OUT_DIR/split.csv (one row per rule, every signal and both verdicts)
and OUT_DIR/split-report.md. Measured in
docs/verification/domain-split-2026-11-03.md.
"""
import collections, csv, glob, json, os, re, sys

out, src = sys.argv[1], os.path.expanduser(sys.argv[2])
here = sys.argv[3]
os.makedirs(here, exist_ok=True)
graph = json.load(open(f"{out}/graph.json"))["nodes"]

def camel_words(name):
    return [w.lower() for w in re.findall(r"[A-Z]+(?![a-z])|[A-Z]?[a-z]+", name)]

# Business vocabulary: words of the GraphQL types the service owns or
# extends (both are subject matter; ownership matters for concepts, not for
# this split), entity names, and role nouns. Generic schema words removed.
GENERIC = {
    "input", "mutation", "query", "error", "code", "response", "filter",
    "filters", "page", "type", "id", "ids", "v", "context", "result", "results",
    "payload", "order", "for", "and", "the", "to", "of", "by", "in", "on", "or",
    "with", "from", "is", "can", "all", "has", "get", "set", "info", "data",
    "item", "items", "node", "level", "why", "active", "update", "create",
    "delete", "add", "remove", "sync", "observe", "queue", "send", "check",
    "complete", "accept", "pin", "unpin", "link", "submit", "devadmin",
    "validation", "lifecycle", "strategy", "picker", "change", "status",
    "connection", "edge", "edges", "count", "list", "map", "config",
    "configuration", "options", "mobile", "navigation", "render",
    # generic words that also name platform things (measured on coaches)
    "request", "requests", "name", "names", "cache", "cached", "field", "fields",
    "record", "records", "event", "events", "current", "start", "publish", "token",
    "search", "under", "admin", "value", "values", "key", "keys", "time", "date",
    "message", "messages", "option", "settings", "setting", "details", "detail",
    "summary", "output", "url", "link", "links", "source", "target", "version", "user", "users",
}
business = set()
for f in glob.glob(f"{src}/*.graphql"):
    for kind, name in re.findall(
        r"^(?:extend\s+)?(type|enum|union|interface|input)\s+([A-Z]\w*)",
        open(f).read(), re.M,
    ):
        business.update(camel_words(name))
for n in graph.values():
    if n["type"] == "entity":
        business.update(re.split(r"[-_.]", os.path.basename(n["file"])[:-3]))
business.update({
    "student", "students", "teacher", "teachers", "coach", "coaches", "parent",
    "parents", "classroom", "classrooms", "district", "districts", "learner",
    "assignments", "assigned", "assignee", "due", "archived", "scheduled",
    "draft", "mastery", "unit", "course", "lesson", "exercise", "essay",
})
business -= {"due", "validate", "custom", "assign", "base", "test", "state", "new", "entity", "typename", "representation"}  # "due to", generic verbs
business -= GENERIC
business = {w for w in business if len(w) >= 3}

TECH = re.compile(r"(?i)\b(" + "|".join([
    r"cach(e|es|ed|ing)", r"connections?", r"pool(s|ed)?", r"retr(y|ies|ied)",
    r"timeouts?", r"circuit", r"breaker", r"log(s|ging)?", r"logged(?! in)", r"metrics?",
    r"trac(e|es|ing)", r"spans?", r"headers?", r"https?", r"grpc",
    r"status code", r"sql", r"pgx", r"alloydb", r"datastore", r"memorystore",
    r"redis", r"gcs", r"buckets?", r"secrets?", r"environment", r"env",
    r"panic(s|ked)?", r"goroutines?", r"marshal\w*", r"unmarshal\w*", r"json",
    r"proto(buf)?", r"credentials?", r"cookies?", r"middleware", r"rate limit\w*",
    r"lint(er|ers|ing)?", r"emulator", r"opentelemetry", r"stackdriver", r"fastly",
    r"transactions?", r"query plan\w*", r"serializ\w*", r"deserializ\w*",
    r"encod(e|es|ed|ing)", r"decod(e|es|ed|ing)", r"kacontext",
    r"service discovery", r"hostname", r"url", r"endpoint url",
    r"batch size", r"backoff", r"latency", r"sampl(e|ed|ing) rate", r"debug",
]) + r")\b")
TECH_QA = {"performance_efficiency", "reliability", "availability",
           "scalability", "portability", "maintainability", "compatibility"}
ACCESS = re.compile(
    r"(?i)isServiceAdmin|IsCurrentUser|IsCoachedByActor|\bcaller\b|\bacl\b|"
    r"hasPermission|permission|authoriz|unauthori|logged in|teacher of|phantom")
AUDIT = re.compile(r"(?i)change event|audit")
BOUNDARY = re.compile(
    r"(?i)http status|status code|grpc|wrap(s|ped)? (the )?error|internal error|"
    r"database error|sql error|translate[sd]? (the )?error")

FAIL = re.compile(
    r"(?i)\b(fail(s|ed|ure|ing)?|returns? an? (\w+ )?error|error (occurs|is returned)|"
    r"encounter(s|ed)? an? (\w+ )?error|unmarshal\w*|decod\w* (fails|the \w+ fails))\b")
PROPAGATE = re.compile(
    r"(?i)(internal( server)? error|transient|wrap\w*|\blog\w*\b|nack|negatively acknowledge|"
    r"\bretry|status code|\b5\d\d\b|propagat\w*|(return|respond with|record|report) "
    r"(the|that|this|an?) (\w+ ){0,3}(error|failure)|panic|acknowledge|"
    r"returns? (false|nil|true|zero|an empty \w+) (with no|without an?) error|bad request status)")
# Operational limits in a trigger or condition: caps, page sizes, batches,
# transport tokens.
LIMIT = re.compile(
    r"(?i)\b(exceeds?|maximum|minimum|max\w*|limit\w*|page size|batch\w*|cap(s|ped)?|"
    r"token|query string|url query|cursor\w*|paginat\w*|offset)\b")
DOMAIN_RESP = re.compile(r"(?i)not found|invalid input|unauthori[sz]ed|forbidden|permission")

def boundary_rule(ears):
    trigger = " ".join(filter(None, [ears.get("if", ""), ears.get("when", "")]))
    shall = [x.get("text", "") for x in ears.get("shall", [])]
    return bool(FAIL.search(trigger)) and bool(shall) and all(
        PROPAGATE.search(x) and not DOMAIN_RESP.search(x) for x in shall)

def frontmatter(text):
    fm = text.split("---", 2)[1]
    keys = dict(re.findall(r"^([a-z_]+): ?(.*)$", fm, re.M))
    roots = re.findall(r"^    root: (\S+)", fm, re.M)
    paths = re.findall(r"^  - path: (\S+)", fm, re.M)
    return keys, paths, roots

rows = []
for rid, n in sorted(graph.items()):
    if n["type"] != "rule":
        continue
    text = open(f"{out}/{n['file']}").read()
    keys, paths, roots = frontmatter(text)
    m = re.search(r"\*\*Requirement \(EARS\)\*\*: (.*)", text)
    req = m.group(1) if m else ""
    edges = n.get("edges", [])
    domain = next(e["to"][7:] for e in edges if e["type"] == "belongs_to")
    idents = " ".join(" ".join(camel_words(t)) for t in re.findall(r"[A-Za-z]\w*", req))
    words = set(re.findall(r"[^\W_]+", (req + " " + idents).lower()))
    if re.search(r"(?i)\bdue (date|dates|soon|today)\b", req): words.add("due")
    cond_vars = re.findall(r'\\"var2?\\":\\"([^\\]+)', keys.get("condition", ""))
    biz_vars = [v for v in cond_vars if not v.startswith("local.") and (
        v.startswith("models.") or business & set(camel_words(v.split(".")[-1]) + v.lower().split(".")))]
    first = req.split(" shall ", 1)[1].split()[0].lower() if " shall " in req else ""

    s = {
        "entity": any(e["type"] == "touches" for e in edges),
        "endpoint": any(e["type"] == "exposed_by" for e in edges),
        "vars": bool(biz_vars),
        "terms": sorted(words & business),
    }
    t = {
        "terms": sorted({x.group(0).lower() for x in TECH.finditer(req)}),
        "quality": keys.get("quality_attribute", "") in TECH_QA,
        "verb": first in {"log", "panic"},
    }
    sm = s["entity"] or s["endpoint"] or s["vars"] or bool(s["terms"])
    tech = bool(t["terms"]) or t["quality"] or t["verb"]
    split = {(True, False): "subject", (False, True): "technical",
             (True, True): "mixed", (False, False): "none"}[(sm, tech)]
    structural = s["entity"] or s["endpoint"] or s["vars"]
    location = "pkg" if roots and len(roots) == len(paths) else (
        "mixed" if roots else "service")
    ears = n.get("ears", {})
    boundary = boundary_rule(ears)
    # What a mixed rule is *about* is in its trigger and conditions; technical
    # words only in the response are implementation detail of a business rule.
    trigger = " ".join(ears.get("where", []) + ears.get("while", []) +
                       [ears.get("when", ""), ears.get("if", "")])
    trig_words = set(re.findall(r"[^\W_]+", (trigger + " " + " ".join(
        " ".join(camel_words(t)) for t in re.findall(r"[A-Za-z]\w*", trigger))).lower()))
    trig_tech = bool(TECH.search(trigger) or LIMIT.search(trigger))
    trig_biz = bool(trig_words & business)
    about = ("technical" if trig_tech and not trig_biz else
             "subject" if trig_biz and not trig_tech else
             "question" if trig_tech else None)
    # Location-free verdict: what the evidence alone says (robustness check).
    if split in ("subject", "technical"):
        verdict_nl = split
    elif split == "mixed":
        verdict_nl = about or ("subject" if structural else "question")
    else:
        verdict_nl = "question"
    if split in ("subject", "technical"):
        verdict = split
    elif split == "mixed":
        verdict = about or ("subject" if structural and location == "service" else (
            "technical" if not structural and location == "pkg" else "question"))
    else:
        verdict = "technical" if location == "pkg" else "question"
    access = bool(ACCESS.search(req + keys.get("condition", ""))) or \
        rid.split(".")[-1].startswith(("authz-", "unchecked-"))
    if boundary and not access:
        verdict = verdict_nl = "technical"
    facets = [f for f, ok in (
        ("access", access),
        ("audit", bool(AUDIT.search(req))),
        ("boundary", boundary or bool(BOUNDARY.search(req))),
    ) if ok]
    rows.append(dict(
        id=rid, domain=domain, split=split, verdict=verdict, verdict_nl=verdict_nl, boundary=int(boundary), nature=keys.get("nature", ""),
        location=location, quality=keys.get("quality_attribute", ""),
        facets="+".join(facets),
        s_entity=int(s["entity"]), s_endpoint=int(s["endpoint"]),
        s_vars=int(s["vars"]), s_terms=" ".join(s["terms"]),
        t_terms=" ".join(t["terms"]), t_quality=int(t["quality"]),
        t_verb=int(t["verb"]), requirement=req,
    ))

with open(f"{here}/split.csv", "w", newline="") as f:
    w = csv.DictWriter(f, fieldnames=list(rows[0]))
    w.writeheader(); w.writerows(rows)

C = collections.Counter
r = []
def table(title, counter, cols):
    r.append(f"\n### {title}\n")
    r.append("| | " + " | ".join(cols) + " | total |")
    r.append("|---" * (len(cols) + 2) + "|")
    for k in sorted({k[0] for k in counter}):
        vals = [counter[(k, c)] for c in cols]
        r.append(f"| {k} | " + " | ".join(map(str, vals)) + f" | {sum(vals)} |")

splits = ["subject", "technical", "mixed", "none"]
r.append(f"# Subject-matter / technical split — {os.path.basename(out)}\n")
r.append(f"{len(rows)} rules. Business vocabulary: {len(business)} words.\n")
r.append("| split | rules |\n|---|---|")
for k in splits:
    r.append(f"| {k} | {sum(1 for x in rows if x['split'] == k)} |")
table("Split × LLM nature", C((x["split"], x["nature"]) for x in rows),
      ["business", "technical", "presentation"])
table("Verdict × LLM nature", C((x["verdict"], x["nature"]) for x in rows),
      ["business", "technical", "presentation"])
table("Location-free verdict × verdict", C((x["verdict_nl"], x["verdict"]) for x in rows),
      ["subject", "technical", "question"])
table("Verdict × location", C((x["verdict"], x["location"]) for x in rows),
      ["service", "pkg", "mixed"])
table("Split × location of citations", C((x["split"], x["location"]) for x in rows),
      ["service", "pkg", "mixed"])
table("Facets × split", C((x["facets"] or "-", x["split"]) for x in rows), splits)

r.append("\n### Domains by technical share (≥ 10 rules)\n")
r.append("| domain | rules | subject | technical | mixed | none | nature=business |")
r.append("|---|---|---|---|---|---|---|")
by = collections.defaultdict(list)
for x in rows: by[x["domain"]].append(x)
for d, xs in sorted(by.items(), key=lambda kv: -sum(x["split"] == "technical" for x in kv[1]) / len(kv[1])):
    if len(xs) < 10: continue
    c = C(x["split"] for x in xs)
    r.append(f"| {d} | {len(xs)} | {c['subject']} | {c['technical']} | {c['mixed']} | {c['none']} | "
             f"{sum(x['nature'] == 'business' for x in xs)} |")

def sample(title, pred, k=8):
    xs = [x for x in rows if pred(x)]
    r.append(f"\n### {title} ({len(xs)})\n")
    for x in xs[:: max(1, len(xs) // k)][:k]:
        ev = f"S: {x['s_terms'] or '-'} | T: {x['t_terms'] or '-'}"
        r.append(f"- `{x['id']}` [{x['nature']}] {ev}\n  {x['requirement'][:220]}")

sample("boundary error handling in service code", lambda x: x["boundary"] and x["location"] == "service")
sample("location-free says question, location decides", lambda x: x["verdict_nl"] == "question" and x["verdict"] != "question")
sample("question", lambda x: x["verdict"] == "question")
sample("technical, but nature=business", lambda x: x["split"] == "technical" and x["nature"] == "business")
sample("subject, but nature=technical", lambda x: x["split"] == "subject" and x["nature"] == "technical")
sample("mixed", lambda x: x["split"] == "mixed")
sample("none (no evidence either way)", lambda x: x["split"] == "none")
sample("subject, cited only in pkg", lambda x: x["split"] == "subject" and x["location"] == "pkg")
open(f"{here}/split-report.md", "w").write("\n".join(r) + "\n")
print("\n".join(r))
