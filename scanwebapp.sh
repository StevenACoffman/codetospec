#!/usr/bin/env bash
# scanwebapp.sh — generate a codetospec spec for one Khan/webapp Go service.
#
# Usage: ./scanwebapp.sh <service> [extra codetospec run flags…]
#        ./scanwebapp.sh pkg [flags…]     # webapp's shared code, scanned once
#   e.g. ./scanwebapp.sh users
#        ./scanwebapp.sh users --crosscheck
#        DRY_RUN=1 ./scanwebapp.sh users      # build + extract only, no LLM call
#        SCIP=0 ./scanwebapp.sh users         # skip the scip-go index
#        REDUCE_MODE=full ./scanwebapp.sh users    # LLM consolidates every rule
#        ./scanwebapp.sh users --reground   # re-ask cached chunks whose rules
#                                           # state values their code lacks
#
# Steps:
#   1. build bin/codetospec and bin/webapp-extract (extractors/webapp);
#   2. run the webapp extractor on $WEBAPP_DIR/services/<service>: mechanical
#      authorization and rate-limit rules, schema-file domains, GraphQL
#      endpoints, Datastore entities, schema docs, cross-service calls and
#      the pkg/ declarations the service reaches; the Go extractor for
#      net/http routes (pub/sub push handlers, REST endpoints), numeric
#      constants, precise symbols and call edges (pkg too); and, with SCIP=1,
#      index the service with scip-go;
#   3. run codetospec on the service with those facts, against Gemini's
#      OpenAI-compatible endpoint, with the key from $GEMINI_API_KEY.
#
# Shared code: `./scanwebapp.sh pkg` scans $WEBAPP_DIR/pkg as a spec of its
# own (~/spec-graphs/pkg: no service extractors). A service scan then
# inherits that spec's rules about the pkg/ code the service reaches
# (codetospec --inherit; cited @pkg/…, labeled inherited_from: pkg) — scan
# pkg first, and rescan services after rescanning it.
#
# Environment (all optional except the API key):
#   GEMINI_API_KEY   Gemini key (or LLM_API_KEY); never printed
#   WEBAPP_DIR       webapp checkout            (default: ~/khan/webapp)
#   OUT_DIR          output graph directory     (default: ~/spec-graphs/<service>)
#   LLM_MODEL        Gemini model id            (default: gemini-3.8-flash)
#   SPEC_LANG        output language, fr|en     (default: en)
#   WORKERS          parallel LLM calls         (default: 4; lower on 429s)
#   MAX_TOKENS       generation cap per call    (default: 16384; thinking models)
#   EXCLUDE          comma-separated excludes   (default: see below)
#   EXTRA_EXCLUDE    appended to the excludes, e.g. one-off tools that are
#                    not product behavior: EXTRA_EXCLUDE='seed*,backfills.go'
#                    (names or globs, matched on file and directory names)
#   REDUCE_MODE      merge|full                 (default: merge — only
#                    near-duplicate candidates go to the LLM, the others keep
#                    no acceptance criteria; full: the LLM consolidates all)
#   SCIP             1|0                        (default: 0 — the Go extractor
#                    gives precise symbols; 1 also indexes with scip-go)
#   PKG_GRAPH        pkg spec to inherit from   (default: <OUT_DIR>/../pkg
#                    when it exists; empty disables inheriting)
#   COVER            coverage profile of the service's tests, made at this
#                    commit (go test -coverpkg=./... -coverprofile=FILE ./...):
#                    rules get `executed: k/n`; files changed since are left out
#   DRY_RUN=1        stop before calling the LLM, print the command
#
# Note: codetospec counts prompt+completion tokens only; Gemini also bills
# thinking tokens — check the Gemini usage page for the real cost.
set -euo pipefail

# has_cmd NAME — true if NAME is an executable file on $PATH.
# Ignores shell functions, aliases, and builtins of the same name.
has_cmd() {
    if [ -n "${ZSH_VERSION:-}" ]; then
        builtin whence -p -- "$1" >/dev/null 2>&1
    elif [ -n "${BASH_VERSION:-}" ]; then
        builtin type -P -- "$1" >/dev/null 2>&1
    else
        command -v -- "$1" >/dev/null 2>&1
    fi
}

die() {
    printf 'scanwebapp: %s\n' "$*" >&2
    exit 1
}

usage() {
    sed -n '2,/^set -euo/{/^set -euo/d;s/^# \{0,1\}//;p;}' "$0" >&2
    exit 2
}

# --- arguments -------------------------------------------------------------

[ $# -ge 1 ] || usage
case "$1" in -h | --help) usage ;; esac
service=$1
shift
case "$service" in
    "" | */* | .*) die "service must be a directory name under services/, got '$service'" ;;
esac

repo_dir=$(cd "$(dirname "$0")" && pwd)
webapp_dir=${WEBAPP_DIR:-$HOME/khan/webapp}
service_dir=$webapp_dir/services/$service
scip_root=services/$service # what scip-go indexes, relative to webapp
if [ "$service" = pkg ]; then
    # webapp's shared code: a spec of its own, which services inherit from.
    service_dir=$webapp_dir/pkg
    scip_root=pkg
fi
out_dir=${OUT_DIR:-$HOME/spec-graphs/$service}
# The pkg spec a service inherits rules from; unset → the sibling of
# out_dir, empty → none.
pkg_graph=${PKG_GRAPH-$(dirname "$out_dir")/pkg}
model=${LLM_MODEL:-gemini-3.8-flash}
spec_lang=${SPEC_LANG:-en}
workers=${WORKERS:-4}
max_tokens=${MAX_TOKENS:-16384}
reduce_mode=${REDUCE_MODE:-merge}
scip=${SCIP:-0}
scip_go=github.com/scip-code/scip-go/cmd/scip-go@v0.2.7
base_url=https://generativelanguage.googleapis.com/v1beta/openai

# Excluded from the LLM map phase. --exclude replaces codetospec's defaults,
# so vendor/node_modules/dist/build are repeated. Tests (incl. testdata/ and
# BDD *.feature), generated code, mocks, notebooks, Terraform and READMEs only
# cost tokens; schema/config files are read by the extractor, not mapped.
# Files with Go's "Code generated … DO NOT EDIT." header are skipped by
# codetospec itself. *.proto and *.py stay: they yield rules (contracts,
# scripts); drop them with EXTRA_EXCLUDE if unwanted.
default_exclude='vendor,node_modules,dist,build,generated,testutil,integration,testdata'
default_exclude+=',*_test.go,*_gen.go,*.pb.go,mock_*.go,*_mock.go,*_mocks.go'
default_exclude+=',*.md,*.txt,*.json,*.yaml,*.yml,*.sql,*.graphql,*.sh,*.toml'
default_exclude+=',*.png,*.jpg,*.xml,*.gotxt,*.gohtml,Makefile,*.proto.dep'
default_exclude+=',README,*.ipynb,*.tf,*.feature'
exclude=${EXCLUDE:-$default_exclude}
if [ -n "${EXTRA_EXCLUDE:-}" ]; then
    exclude+=",$EXTRA_EXCLUDE"
fi

# --- preflight -------------------------------------------------------------

if has_cmd go; then
    :
else
    die "go is not on PATH; install Go >= 1.27.3 first"
fi
[ -d "$service_dir" ] || die "no such service: $service_dir"
[ "$service" = pkg ] || [ -f "$service_dir/gqlgen.yml" ] ||
    printf 'scanwebapp: note: %s has no gqlgen.yml: only its net/http handlers, access policies and tables get mechanical rules\n' "$service_dir" >&2
case "$spec_lang" in fr | en) ;; *) die "SPEC_LANG must be fr or en, got '$spec_lang'" ;; esac
case "$reduce_mode" in full | merge) ;; *) die "REDUCE_MODE must be full or merge, got '$reduce_mode'" ;; esac
case "$scip" in 0 | 1) ;; *) die "SCIP must be 0 or 1, got '$scip'" ;; esac

api_key=${LLM_API_KEY:-${GEMINI_API_KEY:-}}
if [ -z "${DRY_RUN:-}" ] && [ -z "$api_key" ]; then
    die "set GEMINI_API_KEY (or LLM_API_KEY) to your Gemini API key"
fi

# --- 1. build --------------------------------------------------------------

printf '==> building codetospec and the webapp extractor\n' >&2
(cd "$repo_dir" && go build -o bin/codetospec ./cmd/codetospec)
(cd "$repo_dir/extractors/webapp" && go build -o "$repo_dir/bin/webapp-extract" .)
(cd "$repo_dir/extractors/go" && go build -o "$repo_dir/bin/go-extract" .)
if [ "$scip" = 1 ]; then
    (cd "$repo_dir/extractors/scip" && go build -o "$repo_dir/bin/scip2facts" .)
fi

# --- 2. extract ------------------------------------------------------------

mkdir -p "$out_dir/.codetospec"
facts_args=()
inherit_args=()
if [ "$service" != pkg ]; then
    facts_file=$out_dir/.codetospec/webapp-extractor.facts.json
    if [ -n "$pkg_graph" ] && [ -d "$pkg_graph/nodes/rules" ]; then
        inherit_args=(--inherit "pkg=$pkg_graph")
    fi
    printf '==> extracting %s → %s\n' "$service_dir" "$facts_file" >&2
    "$repo_dir/bin/webapp-extract" -root "$service_dir" -lang "$spec_lang" >"$facts_file"
    count() { grep -o "\"kind\":\"$1\"" "$facts_file" | wc -l | tr -d ' '; }
    printf '    rules %s · domains %s · endpoints %s · entities %s · docs %s · cross-service calls %s · pkg declarations reached %s (inheriting from %s)\n' \
        "$(count rule)" "$(count domain)" "$(count route)" "$(count table)" \
        "$(count doc)" "$(count dependency)" "$(count reach)" "${inherit_args[*]:-nothing}" >&2
    facts_args+=(--facts "$facts_file")

fi

# The Go extractor, for services and pkg alike: net/http routes, numeric
# constants, precise symbols and who calls whom (for domains). Optional: a
# failed package load only loses those facts.
go_file=$out_dir/.codetospec/go.facts.json
cover_args=()
if [ -n "${COVER:-}" ]; then
    cover_args=(-cover "$(cd "$(dirname "$COVER")" && pwd)/$(basename "$COVER")")
fi
if (cd "$service_dir" && "$repo_dir/bin/go-extract" -root . -skip-dirs testutil "${cover_args[@]}" >"$go_file" 2>"$go_file.log"); then
    gcount() { grep -o "\"kind\":\"$1\"" "$go_file" | wc -l | tr -d ' '; }
    printf '    routes %s · constants %s · precise symbols %s · calls %s\n' \
        "$(gcount route)" "$(gcount constant)" "$(gcount symbol)" "$(gcount call)" >&2
    facts_args+=(--facts "$go_file")
else
    printf 'scanwebapp: warning: Go extraction failed (see %s); continuing without it\n' "$go_file.log" >&2
fi

if [ "$scip" = 1 ]; then
    scip_index=$out_dir/.codetospec/index.scip
    scip_facts=$out_dir/.codetospec/scip.facts.json
    printf '==> indexing %s with scip-go (first run downloads it)\n' "$scip_root" >&2
    (cd "$webapp_dir" && go run "$scip_go" index --quiet --skip-tests \
        --output "$scip_index" "./$scip_root/...")
    "$repo_dir/bin/scip2facts" --index "$scip_index" --root "$scip_root" >"$scip_facts"
    printf '    %s precise symbols\n' "$(grep -o '"kind":"symbol"' "$scip_facts" | wc -l | tr -d ' ')" >&2
    facts_args+=(--facts "$scip_facts")
fi

# --- 3. run codetospec -----------------------------------------------------

cmd=(
    "$repo_dir/bin/codetospec" run
    --src "$service_dir"
    --ref-root "pkg=$webapp_dir/pkg"
    "${inherit_args[@]}"
    --out "$out_dir"
    --lang "$spec_lang"
    --max-tokens "$max_tokens"
    --workers "$workers"
    --exclude "$exclude"
    --reduce-mode "$reduce_mode"
    "${facts_args[@]}"
    "$@"
)

if [ -n "${DRY_RUN:-}" ]; then
    printf '==> DRY_RUN: would run (model %s):\n   ' "$model" >&2
    printf ' %q' "${cmd[@]}" >&2
    printf '\n' >&2
    exit 0
fi

printf '==> running codetospec on %s with %s (reduce: %s, scip: %s; output: %s)\n' \
    "$service" "$model" "$reduce_mode" "$scip" "$out_dir" >&2
LLM_BASE_URL=$base_url LLM_MODEL=$model LLM_API_KEY=$api_key "${cmd[@]}"

printf '==> done\n' >&2
"$repo_dir/bin/codetospec" stats --out "$out_dir"
