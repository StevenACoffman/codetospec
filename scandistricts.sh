#!/usr/bin/env bash
# scandistricts.sh — generate codetospec specs for the districts-jobs Go
# workspace: one per module, each with the rules of the shared pkg/ code it
# uses.
#
# Usage: ./scandistricts.sh pkg [extra codetospec run flags…]       # shared code, first
#        ./scandistricts.sh <module> [flags…]                      # one module of go.work
#        ./scandistricts.sh all [flags…]                           # pkg, then every module
#   e.g. ./scandistricts.sh roster --crosscheck
#        DRY_RUN=1 ./scandistricts.sh roster   # build + extract only, no LLM call
#
# Steps, for one module:
#   1. build bin/codetospec, bin/go-extract (and bin/scip2facts with SCIP=1);
#   2. run the Go extractor on the module: net/http routes, numeric
#      constants, SQL tables, precise symbols, call edges, and a reach fact
#      for every pkg/ declaration the module's code reaches (with -shared
#      pkg=…); with SCIP=1, also index the module with scip-go;
#   3. run codetospec on the module: its own rules from the LLM, plus the
#      rules of the pkg spec (--inherit) whose citation lies in reached pkg/
#      code, cited in pkg/ (--ref-root) and labeled inherited_from: pkg.
#
# Scan pkg first, and rescan the modules after rescanning it: a module
# inherits whatever pkg spec is on disk.
#
# Environment (all optional except the API key):
#   GEMINI_API_KEY   Gemini key (or LLM_API_KEY); never printed
#   DISTRICTS_DIR    workspace checkout     (default: ~/Documents/git/districts-jobs)
#   OUT_ROOT         specs directory        (default: ~/spec-graphs/districts-jobs;
#                    one subdirectory per module, pkg included. Not
#                    ~/spec-graphs/districts: scanwebapp.sh writes the webapp
#                    districts service there)
#   LLM_MODEL        Gemini model id        (default: gemini-3.8-flash)
#   SPEC_LANG        output language, fr|en (default: en)
#   WORKERS          parallel LLM calls     (default: 4; lower on 429s)
#   MAX_TOKENS       generation cap per call (default: 16384; thinking models)
#   EXCLUDE          comma-separated excludes (default: see below)
#   EXTRA_EXCLUDE    appended to the excludes (names or globs)
#   REDUCE_MODE      merge|full             (default: merge)
#   SCIP             1|0                    (default: 0 — the Go extractor gives
#                    precise symbols; 1 also indexes with scip-go)
#   COVER            coverage profile of the module's tests, made at this
#                    commit (go test -coverpkg=./... -coverprofile=FILE ./...):
#                    rules get `executed: k/n`; files of other modules are
#                    ignored, files changed since the profile left out
#   DRY_RUN=1        stop before calling the LLM, print the command
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
    printf 'scandistricts: %s\n' "$*" >&2
    exit 1
}

usage() {
    sed -n '2,/^set -euo/{/^set -euo/d;s/^# \{0,1\}//;p;}' "$0" >&2
    exit 2
}

# workspace_modules lists the modules go.work uses, one per line, without
# the leading "./".
workspace_modules() {
    sed -n '/^use (/,/^)/{s/^[[:space:]]*\.\/\([^[:space:]]*\).*/\1/p;}' "$1/go.work"
}

# --- arguments -------------------------------------------------------------

[ $# -ge 1 ] || usage
case "$1" in -h | --help) usage ;; esac
module=$1
shift

repo_dir=$(cd "$(dirname "$0")" && pwd)
districts_dir=${DISTRICTS_DIR:-$HOME/Documents/git/districts-jobs}
out_root=${OUT_ROOT:-$HOME/spec-graphs/districts-jobs}
[ -f "$districts_dir/go.work" ] || die "no go.work in $districts_dir (set DISTRICTS_DIR)"

# build compiles codetospec and the extractors once per invocation: `all`
# builds, then its per-module runs skip it (_SCANDISTRICTS_BUILT).
build() {
    if [ -n "${_SCANDISTRICTS_BUILT:-}" ]; then
        return
    fi
    printf '==> building codetospec and the Go extractor\n' >&2
    (cd "$repo_dir" && go build -o bin/codetospec ./cmd/codetospec)
    (cd "$repo_dir/extractors/go" && go build -o "$repo_dir/bin/go-extract" .)
    if [ "${SCIP:-0}" = 1 ]; then
        (cd "$repo_dir/extractors/scip" && go build -o "$repo_dir/bin/scip2facts" .)
    fi
    export _SCANDISTRICTS_BUILT=1
}

if [ "$module" = all ]; then
    build
    failed=()
    for m in pkg $(workspace_modules "$districts_dir" | grep -vx pkg); do
        printf '\n=== %s\n' "$m" >&2
        "$0" "$m" "$@" || failed+=("$m")
    done
    if [ "${#failed[@]}" -gt 0 ]; then
        die "failed: ${failed[*]}"
    fi
    exit 0
fi

workspace_modules "$districts_dir" | grep -qx -- "$module" ||
    die "'$module' is not a module of $districts_dir/go.work"

module_dir=$districts_dir/$module
pkg_dir=$districts_dir/pkg
out_dir=$out_root/$module
pkg_graph=$out_root/pkg
model=${LLM_MODEL:-gemini-3.8-flash}
spec_lang=${SPEC_LANG:-en}
workers=${WORKERS:-4}
max_tokens=${MAX_TOKENS:-16384}
reduce_mode=${REDUCE_MODE:-merge}
scip=${SCIP:-0}
scip_go=github.com/scip-code/scip-go/cmd/scip-go@v0.2.7
base_url=https://generativelanguage.googleapis.com/v1beta/openai

# Excluded from the LLM map phase (--exclude replaces codetospec's
# defaults): tests, generated code, mocks and non-Go files cost tokens and
# yield no rules of the module. In pkg/, the *test packages are fakes for
# tests (pstest, servicetest…), *demo ones are examples and mages holds
# build tooling: none states a business rule a module could inherit.
default_exclude='vendor,node_modules,dist,build,generated,testutil,integration,testdata,mocks'
default_exclude+=',*test,*demo,mages'
default_exclude+=',*_test.go,*_gen.go,*.pb.go,mock_*.go,*_mock.go,*_mocks.go,magefile.go'
default_exclude+=',*.md,*.txt,*.json,*.yaml,*.yml,*.sql,*.graphql,*.sh,*.toml,*.out'
default_exclude+=',*.png,*.jpg,*.xml,*.gotxt,*.gohtml,Makefile,Dockerfile,*.swagger'
exclude=${EXCLUDE:-$default_exclude}
if [ -n "${EXTRA_EXCLUDE:-}" ]; then
    exclude+=",$EXTRA_EXCLUDE"
fi

# --- preflight -------------------------------------------------------------

if has_cmd go; then
    :
else
    die "go is not on PATH; install Go >= 1.27 first"
fi
case "$spec_lang" in fr | en) ;; *) die "SPEC_LANG must be fr or en, got '$spec_lang'" ;; esac
case "$reduce_mode" in full | merge) ;; *) die "REDUCE_MODE must be full or merge, got '$reduce_mode'" ;; esac
case "$scip" in 0 | 1) ;; *) die "SCIP must be 0 or 1, got '$scip'" ;; esac

api_key=${LLM_API_KEY:-${GEMINI_API_KEY:-}}
if [ -z "${DRY_RUN:-}" ] && [ -z "$api_key" ]; then
    die "set GEMINI_API_KEY (or LLM_API_KEY) to your Gemini API key"
fi

# --- 1. build --------------------------------------------------------------

build

# --- 2. extract ------------------------------------------------------------

mkdir -p "$out_dir/.codetospec"
facts_file=$out_dir/.codetospec/go.facts.json
shared_args=()
inherit_args=()
if [ "$module" != pkg ]; then
    shared_args=(-shared "pkg=$pkg_dir")
    # Reach facts cite pkg/, so its root is always given.
    inherit_args=(--ref-root "pkg=$pkg_dir")
    if [ -d "$pkg_graph/nodes/rules" ]; then
        inherit_args+=(--inherit "pkg=$pkg_graph")
    else
        printf 'scandistricts: note: no pkg spec in %s (run ./scandistricts.sh pkg first); continuing without inherited rules\n' "$pkg_graph" >&2
    fi
fi
printf '==> extracting %s → %s\n' "$module_dir" "$facts_file" >&2
cover_args=()
if [ -n "${COVER:-}" ]; then
    cover_args=(-cover "$(cd "$(dirname "$COVER")" && pwd)/$(basename "$COVER")")
fi
(cd "$module_dir" && "$repo_dir/bin/go-extract" -root . -skip-dirs testutil "${shared_args[@]}" "${cover_args[@]}" >"$facts_file")
count() { grep -o "\"kind\":\"$1\"" "$facts_file" | wc -l | tr -d ' '; }
printf '    routes %s · constants %s · tables %s · symbols %s · calls %s · pkg declarations reached %s\n' \
    "$(count route)" "$(count constant)" "$(count table)" "$(count symbol)" "$(count call)" "$(count reach)" >&2
facts_args=(--facts "$facts_file")

if [ "$scip" = 1 ]; then
    scip_index=$out_dir/.codetospec/index.scip
    scip_facts=$out_dir/.codetospec/scip.facts.json
    printf '==> indexing %s with scip-go (first run downloads it)\n' "$module" >&2
    (cd "$module_dir" && go run "$scip_go" index --quiet --skip-tests --output "$scip_index" ./...)
    "$repo_dir/bin/scip2facts" --index "$scip_index" >"$scip_facts"
    printf '    %s precise symbols\n' "$(grep -o '"kind":"symbol"' "$scip_facts" | wc -l | tr -d ' ')" >&2
    facts_args+=(--facts "$scip_facts")
fi

# --- 3. run codetospec -----------------------------------------------------

cmd=(
    "$repo_dir/bin/codetospec" run
    --src "$module_dir"
    --out "$out_dir"
    --lang "$spec_lang"
    --max-tokens "$max_tokens"
    --workers "$workers"
    --exclude "$exclude"
    --reduce-mode "$reduce_mode"
    "${inherit_args[@]}"
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
    "$module" "$model" "$reduce_mode" "$scip" "$out_dir" >&2
LLM_BASE_URL=$base_url LLM_MODEL=$model LLM_API_KEY=$api_key "${cmd[@]}"

printf '==> done\n' >&2
"$repo_dir/bin/codetospec" stats --out "$out_dir"
