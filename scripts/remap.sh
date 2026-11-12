#!/usr/bin/env bash
# remap.sh — remap every output under ~/spec-graphs and take the
# measurements a remap owes (TODO.md, "Map Input" and "Repository Checks").
#
# Usage: scripts/remap.sh [extra codetospec run flags…]
#        DRY_RUN=1 scripts/remap.sh       # print the steps, call no LLM
#
# Steps:
#   1. preflight: the API key is set, the codetospec tree is clean, the
#      webapp and districts-jobs checkouts are on master or main, and their
#      commits are recorded (the corpus baselines pin them: do not move
#      them during the remap; each step checks they have not moved);
#   2. snapshot ~/spec-graphs to $SNAPSHOT (once: a rerun keeps the first
#      snapshot), the "before" of every comparison;
#   3. pkg, as every service inherits from it; then pkg a second time into
#      $VARIANCE_DIR, fresh, same prompt and model, map and reduce only: the
#      run-to-run variance;
#   4. the webapp services, then districts-jobs (scandistricts.sh all);
#   5. scripts/remapcheck.py on the two pkg runs against the snapshot and
#      the pre-2026-11-02 outputs, into $REPORT;
#   6. re-record the corpus baselines (CODETOSPEC_CORPUS_UPDATE=1 make
#      corpus) and show the manifest's diff: old counts against new, per
#      output. The plain `make corpus` skips every entry once a source has
#      left its recorded commit, so the diff is the comparison. Review it,
#      then commit it or revert it (git checkout the manifest).
#
# A step that fails stops the script; rerunning resumes from the caches
# (every phase is content-addressed), so finished outputs cost nothing.
#
# Environment (all optional except the API key):
#   GEMINI_API_KEY   Gemini key (or LLM_API_KEY); never printed
#   GRAPHS           outputs root          (default: ~/spec-graphs)
#   SNAPSHOT         copy before remapping (default: <GRAPHS>.before-remap-<today>)
#   VARIANCE_DIR     second pkg run        (default: <GRAPHS>.variance/pkg)
#   OLDER            the outputs before the remap that lost technical rules
#                    (default: ~/spec-graphs.before-remap-2026-11-02)
#   REPORT           remapcheck report     (default: <VARIANCE_DIR>/../remap-check-<today>.md)
#   SERVICES         webapp services       (default: admin ai-guide assignments
#                    coaches districts graphql-gateway users)
#   DISTRICTS_JOBS   1|0                   (default: 1 when ~/Documents/git/districts-jobs exists)
#   VARIANCE         1|0                   (default: 1)
#   ALLOW_BRANCH     1 maps a checkout on another branch or a detached
#                    HEAD (default: refused)
#   ACCEPTANCE       1|0                   (default: 1; the slowest pass, about
#                    half of a service's time; only the Quire and Kiro exports
#                    read it, and it can run later, cached per rule)
#   WEBAPP_DIR, LLM_MODEL, WORKERS, … are passed through to scanwebapp.sh
#   and scandistricts.sh.
#
# Default run flags: --quality --crosscheck --acceptance --write-domain-model
# (the last remap's, plus the concepts proposal; ACCEPTANCE=0 drops
# --acceptance). Extra arguments are appended to them.
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

die() { printf 'remap.sh: %s\n' "$*" >&2; exit 1; }
step() { printf '\n==> %s\n' "$*" >&2; }

repo_dir=$(cd "$(dirname "$0")/.." && pwd)
today=$(date +%F)
graphs=${GRAPHS:-$HOME/spec-graphs}
snapshot=${SNAPSHOT:-$graphs.before-remap-$today}
variance_dir=${VARIANCE_DIR:-$graphs.variance/pkg}
older=${OLDER:-$HOME/spec-graphs.before-remap-2026-11-02}
report=${REPORT:-$(dirname "$variance_dir")/remap-check-$today.md}
services=${SERVICES:-admin ai-guide assignments coaches districts graphql-gateway users}
webapp_dir=${WEBAPP_DIR:-$HOME/khan/webapp}
districts_dir=${DISTRICTS_DIR:-$HOME/Documents/git/districts-jobs}
if [ -z "${DISTRICTS_JOBS:-}" ]; then
    if [ -d "$districts_dir" ]; then DISTRICTS_JOBS=1; else DISTRICTS_JOBS=0; fi
fi
flags=(--quality --crosscheck)
if [ "${ACCEPTANCE:-1}" = 1 ]; then flags+=(--acceptance); fi
flags+=(--write-domain-model "$@")

run() {
    printf '    %s\n' "$*" >&2
    if [ -z "${DRY_RUN:-}" ]; then "$@"; fi
}

# --- 1. preflight -----------------------------------------------------------

step "preflight"
for cmd in git go uv make; do
    if has_cmd "$cmd"; then :; else die "$cmd is not on PATH"; fi
done
if [ -z "${DRY_RUN:-}" ] && [ -z "${LLM_API_KEY:-${GEMINI_API_KEY:-}}" ]; then
    die "set GEMINI_API_KEY (or LLM_API_KEY)"
fi
if [ -n "$(git -C "$repo_dir" status --porcelain)" ]; then
    if [ -n "${DRY_RUN:-}" ]; then
        printf '    warning: uncommitted changes (a real run stops here)\n' >&2
    else
        die "the codetospec tree has uncommitted changes: commit them, so the outputs name one revision"
    fi
fi
# heads prints each checkout's commit, one "name commit" line each.
heads() {
    printf 'codetospec %s\n' "$(git -C "$repo_dir" rev-parse HEAD)"
    printf 'webapp %s\n' "$(git -C "$webapp_dir" rev-parse HEAD)"
    if [ "$DISTRICTS_JOBS" = 1 ]; then
        printf 'districts-jobs %s\n' "$(git -C "$districts_dir" rev-parse HEAD)"
    fi
}
start_heads=$(heads)
printf '%s\n' "$start_heads" | sed 's/^/    /' >&2

# A checkout left on a feature branch would be mapped without notice
# (docs/verification/remap-2026-11-12.md): each must be on master or main.
checkouts=("webapp=$webapp_dir")
if [ "$DISTRICTS_JOBS" = 1 ]; then checkouts+=("districts-jobs=$districts_dir"); fi
for pair in "${checkouts[@]}"; do
    name=${pair%%=*}
    branch=$(git -C "${pair#*=}" symbolic-ref --short -q HEAD || printf '(detached)')
    printf '    %s on %s\n' "$name" "$branch" >&2
    case $branch in
    master | main) ;;
    *)
        if [ "${ALLOW_BRANCH:-}" = 1 ]; then
            printf '    warning: %s is on %s (ALLOW_BRANCH=1)\n' "$name" "$branch" >&2
        elif [ -n "${DRY_RUN:-}" ]; then
            printf '    warning: %s is on %s (a real run stops here)\n' "$name" "$branch" >&2
        else
            die "$name is on $branch, not master or main: switch it, or set ALLOW_BRANCH=1"
        fi
        ;;
    esac
done

# unmoved stops the remap when a checkout has moved since the preflight,
# before the next step spends its calls on a mix of commits.
unmoved() {
    if [ -z "${DRY_RUN:-}" ] && [ "$(heads)" != "$start_heads" ]; then
        die "a checkout moved during the remap; the outputs mix commits. Was: $start_heads"
    fi
}

# --- 2. snapshot ------------------------------------------------------------

step "snapshot $graphs → $snapshot"
if [ -e "$snapshot" ]; then
    printf '    kept: %s exists (an earlier run of this remap took it)\n' "$snapshot" >&2
else
    run cp -a "$graphs" "$snapshot"
fi

# --- 3. pkg, twice ------------------------------------------------------------

step "pkg"
unmoved
run "$repo_dir/scanwebapp.sh" pkg "${flags[@]}"
if [ "${VARIANCE:-1}" = 1 ]; then
    step "pkg again into $variance_dir (run-to-run variance; map and reduce only)"
    unmoved
    run env OUT_DIR="$variance_dir" PKG_GRAPH= "$repo_dir/scanwebapp.sh" pkg
fi

# --- 4. services, districts-jobs ---------------------------------------------

for svc in $services; do
    step "$svc"
    unmoved
    run "$repo_dir/scanwebapp.sh" "$svc" "${flags[@]}"
done
if [ "$DISTRICTS_JOBS" = 1 ]; then
    step "districts-jobs"
    unmoved
    run "$repo_dir/scandistricts.sh" all "${flags[@]}"
fi

# --- 5. measurements ----------------------------------------------------------

if [ "${VARIANCE:-1}" = 1 ]; then
    step "remap check → $report"
    baselines=("snapshot=$snapshot/pkg")
    if [ -d "$older/pkg" ]; then baselines+=("before-loss=$older/pkg"); fi
    if [ -z "${DRY_RUN:-}" ]; then
        uv run --no-project "$repo_dir/scripts/remapcheck.py" \
            "$graphs/pkg" "$variance_dir" "${baselines[@]}" >"$report"
        cat "$report" >&2
    else
        printf '    uv run scripts/remapcheck.py %s %s %s > %s\n' \
            "$graphs/pkg" "$variance_dir" "${baselines[*]}" "$report" >&2
    fi
fi

# --- 6. corpus ------------------------------------------------------------

unmoved
manifest=internal/corpus/testdata/corpus.json
step "re-record the corpus baselines; old against new in the diff of $manifest"
if [ -z "${DRY_RUN:-}" ]; then
    (cd "$repo_dir" && CODETOSPEC_CORPUS_UPDATE=1 make corpus)
    git -C "$repo_dir" --no-pager diff -- "$manifest" >&2
fi
cat >&2 <<EOF

Done. Next, by hand:
  - read $report; record it as a docs/verification file;
  - review the diff of $manifest: commit it, or revert it with
      git checkout -- $manifest
  - tick the remap items of TODO.md.
EOF
