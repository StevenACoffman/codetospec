#!/usr/bin/env bash
# scanwebapp.sh — generate a codetospec spec for one Khan/webapp Go service.
#
# Usage: ./scanwebapp.sh <service> [extra codetospec run flags…]
#   e.g. ./scanwebapp.sh users
#        ./scanwebapp.sh users --crosscheck
#        DRY_RUN=1 ./scanwebapp.sh users      # build + extract only, no LLM call
#        SCIP=0 ./scanwebapp.sh users         # skip the scip-go index
#        REDUCE_MODE=full ./scanwebapp.sh users    # LLM consolidates every rule
#
# Steps:
#   1. build bin/codetospec and bin/webapp-extract (extractors/webapp);
#   2. run the webapp extractor on $WEBAPP_DIR/services/<service>: mechanical
#      authorization rules, schema-file domains, GraphQL endpoints, Datastore
#      entities, numeric constants, schema docs and cross-service calls;
#      and index the service with scip-go for precise symbols (SCIP=0 skips);
#   3. run codetospec on the service with those facts, against Gemini's
#      OpenAI-compatible endpoint, with the key from $GEMINI_API_KEY.
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
#   SCIP             1|0                        (default: 1 — index with scip-go
#                    for precise citation spans; first run downloads it)
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
out_dir=${OUT_DIR:-$HOME/spec-graphs/$service}
model=${LLM_MODEL:-gemini-3.8-flash}
spec_lang=${SPEC_LANG:-en}
workers=${WORKERS:-4}
max_tokens=${MAX_TOKENS:-16384}
reduce_mode=${REDUCE_MODE:-merge}
scip=${SCIP:-1}
scip_go=github.com/scip-code/scip-go/cmd/scip-go@v0.2.7
base_url=https://generativelanguage.googleapis.com/v1beta/openai

# Excluded from the LLM map phase. --exclude replaces codetospec's defaults,
# so vendor/node_modules/dist/build are repeated. Tests, generated code,
# mocks and test harnesses only cost tokens; schema/config files are read by
# the extractor, not mapped.
default_exclude='vendor,node_modules,dist,build,generated,testutil,integration'
default_exclude+=',*_test.go,*_gen.go,*.pb.go,mock_*.go,*_mock.go,*_mocks.go'
default_exclude+=',*.md,*.txt,*.json,*.yaml,*.yml,*.sql,*.graphql,*.sh,*.toml'
default_exclude+=',*.png,*.jpg,*.xml,*.gotxt,*.gohtml,Makefile,*.proto.dep'
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
[ -d "$service_dir" ] || die "no such service: $service_dir"
[ -f "$service_dir/gqlgen.yml" ] ||
    printf 'scanwebapp: warning: %s has no gqlgen.yml; resolvers keep their directory domain\n' "$service_dir" >&2
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
if [ "$scip" = 1 ]; then
    (cd "$repo_dir/extractors/scip" && go build -o "$repo_dir/bin/scip2facts" .)
fi

# --- 2. extract ------------------------------------------------------------

mkdir -p "$out_dir/.codetospec"
facts_file=$out_dir/.codetospec/webapp-extractor.facts.json
printf '==> extracting %s → %s\n' "$service_dir" "$facts_file" >&2
"$repo_dir/bin/webapp-extract" -root "$service_dir" -lang "$spec_lang" >"$facts_file"
count() { grep -o "\"kind\":\"$1\"" "$facts_file" | wc -l | tr -d ' '; }
printf '    rules %s · domains %s · endpoints %s · entities %s · constants %s · docs %s · cross-service calls %s\n' \
    "$(count rule)" "$(count domain)" "$(count route)" "$(count table)" \
    "$(count constant)" "$(count doc)" "$(count dependency)" >&2
facts_args=(--facts "$facts_file")

if [ "$scip" = 1 ]; then
    scip_index=$out_dir/.codetospec/index.scip
    scip_facts=$out_dir/.codetospec/scip.facts.json
    printf '==> indexing services/%s with scip-go (first run downloads it)\n' "$service" >&2
    (cd "$webapp_dir" && go run "$scip_go" index --quiet --skip-tests \
        --output "$scip_index" "./services/$service/...")
    "$repo_dir/bin/scip2facts" --index "$scip_index" --root "services/$service" >"$scip_facts"
    printf '    %s precise symbols\n' "$(grep -o '"kind":"symbol"' "$scip_facts" | wc -l | tr -d ' ')" >&2
    facts_args+=(--facts "$scip_facts")
fi

# --- 3. run codetospec -----------------------------------------------------

cmd=(
    "$repo_dir/bin/codetospec" run
    --src "$service_dir"
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
