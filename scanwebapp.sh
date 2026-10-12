#!/usr/bin/env bash
# scanwebapp.sh — generate a codetospec spec for one Khan/webapp Go service.
#
# Usage: ./scanwebapp.sh <service> [extra codetospec run flags…]
#   e.g. ./scanwebapp.sh users
#        ./scanwebapp.sh users --crosscheck
#        DRY_RUN=1 ./scanwebapp.sh users      # build + extract only, no LLM call
#
# Steps:
#   1. build bin/codetospec and bin/webapp-extract (extractors/webapp);
#   2. run the webapp extractor on $WEBAPP_DIR/services/<service>: mechanical
#      authorization rules for gqlgen resolvers + schema-file domains;
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
base_url=https://generativelanguage.googleapis.com/v1beta/openai

# Excluded from the LLM map phase. --exclude replaces codetospec's defaults,
# so vendor/node_modules/dist/build are repeated. Tests, generated code,
# mocks and test harnesses only cost tokens; schema/config files are read by
# the extractor, not mapped.
default_exclude='vendor,node_modules,dist,build,generated,testutil,integration'
default_exclude+=',*_test.go,*_gen.go,*.pb.go,mock_*.go,*_mock.go,*_mocks.go'
default_exclude+=',*.md,*.txt,*.json,*.yaml,*.yml,*.sql,*.graphql,*.sh,*.toml'
default_exclude+=',*.png,*.jpg,*.xml,*.gotxt,*.gohtml'
exclude=${EXCLUDE:-$default_exclude}

# --- preflight -------------------------------------------------------------

if has_cmd go; then
    :
else
    die "go is not on PATH; install Go >= 1.26 first"
fi
[ -d "$service_dir" ] || die "no such service: $service_dir"
[ -f "$service_dir/gqlgen.yml" ] ||
    printf 'scanwebapp: warning: %s has no gqlgen.yml; resolvers keep their directory domain\n' "$service_dir" >&2
case "$spec_lang" in fr | en) ;; *) die "SPEC_LANG must be fr or en, got '$spec_lang'" ;; esac

api_key=${LLM_API_KEY:-${GEMINI_API_KEY:-}}
if [ -z "${DRY_RUN:-}" ] && [ -z "$api_key" ]; then
    die "set GEMINI_API_KEY (or LLM_API_KEY) to your Gemini API key"
fi

# --- 1. build --------------------------------------------------------------

printf '==> building codetospec and the webapp extractor\n' >&2
(cd "$repo_dir" && go build -o bin/codetospec ./cmd/codetospec)
(cd "$repo_dir/extractors/webapp" && go build -o "$repo_dir/bin/webapp-extract" .)

# --- 2. extract ------------------------------------------------------------

mkdir -p "$out_dir/.codetospec"
facts_file=$out_dir/.codetospec/webapp-extractor.facts.json
printf '==> extracting %s → %s\n' "$service_dir" "$facts_file" >&2
"$repo_dir/bin/webapp-extract" -root "$service_dir" -lang "$spec_lang" >"$facts_file"
rules=$(grep -o '"kind":"rule"' "$facts_file" | wc -l | tr -d ' ')
domains=$(grep -o '"kind":"domain"' "$facts_file" | wc -l | tr -d ' ')
printf '    %s mechanical rules, %s resolver methods mapped to schema-file domains\n' "$rules" "$domains" >&2

# --- 3. run codetospec -----------------------------------------------------

cmd=(
    "$repo_dir/bin/codetospec" run
    --src "$service_dir"
    --out "$out_dir"
    --lang "$spec_lang"
    --max-tokens "$max_tokens"
    --workers "$workers"
    --exclude "$exclude"
    --facts "$facts_file"
    "$@"
)

if [ -n "${DRY_RUN:-}" ]; then
    printf '==> DRY_RUN: would run (model %s):\n   ' "$model" >&2
    printf ' %q' "${cmd[@]}" >&2
    printf '\n' >&2
    exit 0
fi

printf '==> running codetospec on %s with %s (output: %s)\n' "$service" "$model" "$out_dir" >&2
LLM_BASE_URL=$base_url LLM_MODEL=$model LLM_API_KEY=$api_key "${cmd[@]}"

printf '==> done\n' >&2
"$repo_dir/bin/codetospec" stats --out "$out_dir"
