#!/usr/bin/env bash
# scanwebapp-authz.sh — list the authorization rules of all of webapp in one
# directory, without an LLM: the mechanical rules of every Go service and of
# the shared code they reach, as a page, a CSV and a Markdown listing.
#
# Usage: ./scanwebapp-authz.sh [OUT_DIR]          (default: ~/authz-webapp)
#
# Steps:
#   1. build the webapp and Go extractors into OUT_DIR/.bin — never this
#      repo's bin/, which a running scanwebapp.sh rebuilds;
#   2. webapp-extract -all on the checkout → OUT_DIR/webapp-authz.facts.json;
#   3. the Go extractor per Go service (net/http routes, so handlers count
#      as entry points) → OUT_DIR/routes/<service>.facts.json, JOBS at a
#      time; a service whose packages do not load only loses its routes;
#   4. scripts/authzall.py → OUT_DIR/authz.html, authz.csv, authz.md.
#
# Environment:
#   WEBAPP_DIR   webapp checkout            (default: ~/khan/webapp)
#   SPEC_LANG    rule language, en or fr    (default: en)
#   ROUTES       0 skips step 3             (default: 1)
#   JOBS         parallel route extractions (default: 4)
set -euo pipefail

# --routes-of SERVICE: step 3 for one service (the script runs itself in
# parallel, with AUTHZ_BIN, AUTHZ_OUT and WEBAPP_DIR set by the main run).
if [ "${1:-}" = --routes-of ]; then
    service=$2 webapp=${WEBAPP_DIR:?} bin=${AUTHZ_BIN:?} out=${AUTHZ_OUT:?}
    if ! (cd "$webapp/services/$service" &&
        "$bin/go-extract" -root . -skip-dirs testutil \
            >"$out/routes/$service.facts.json" 2>"$out/routes/$service.log"); then
        rm -f "$out/routes/$service.facts.json"
        printf '    %s: routes skipped (see routes/%s.log)\n' "$service" "$service" >&2
    fi
    exit 0
fi

repo_dir=$(cd "$(dirname "$0")" && pwd)
webapp_dir=${WEBAPP_DIR:-$HOME/khan/webapp}
out_dir=${1:-$HOME/authz-webapp}
spec_lang=${SPEC_LANG:-en}
jobs=${JOBS:-4}
bin_dir=$out_dir/.bin

[ -d "$webapp_dir/services" ] || {
    printf 'scanwebapp-authz: %s is not a webapp checkout (no services/)\n' "$webapp_dir" >&2
    exit 2
}
mkdir -p "$bin_dir" "$out_dir/routes"

printf '==> building the extractors into %s\n' "$bin_dir" >&2
(cd "$repo_dir/extractors/webapp" && go build -o "$bin_dir/webapp-extract" .)
(cd "$repo_dir/extractors/go" && go build -o "$bin_dir/go-extract" .)

facts_file=$out_dir/webapp-authz.facts.json
printf '==> extracting every Go service of %s\n' "$webapp_dir" >&2
"$bin_dir/webapp-extract" -all -root "$webapp_dir" -lang "$spec_lang" \
    >"$facts_file" 2>"$out_dir/webapp-authz.log"
printf '    %s rules (%s warnings, see webapp-authz.log)\n' \
    "$(grep -o '"kind":"rule"' "$facts_file" | wc -l | tr -d ' ')" \
    "$(wc -l <"$out_dir/webapp-authz.log" | tr -d ' ')" >&2

routes_args=()
if [ "${ROUTES:-1}" = 1 ]; then
    printf '==> extracting net/http routes, %s services at a time\n' "$jobs" >&2
    for dir in "$webapp_dir"/services/*/; do
        # a Go service: some non-test Go file outside vendored and test code
        if find "$dir" -name '*.go' -not -name '*_test.go' -not -path '*/testdata/*' \
            -not -path '*/node_modules/*' -print -quit | grep -q .; then
            basename "$dir"
        fi
    done | WEBAPP_DIR=$webapp_dir AUTHZ_BIN=$bin_dir AUTHZ_OUT=$out_dir \
        xargs -P "$jobs" -n 1 "$0" --routes-of
    routes_args=(--routes-dir "$out_dir/routes")
fi

printf '==> writing the listing\n' >&2
(cd "$repo_dir" && uv run scripts/authzall.py --facts "$facts_file" "${routes_args[@]}" \
    --webapp "$webapp_dir" -o "$out_dir")
printf '==> done: %s/authz.html, authz.csv, authz.md\n' "$out_dir" >&2
