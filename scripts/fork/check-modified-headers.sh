#!/usr/bin/env bash
# G8: every upstream file the fork modified says so. A modified file (status M
# against the baseline) needs the header line in its first 5 lines, or a row in
# MODIFICATIONS.md (files that cannot hold a comment, and regenerated files).
#
#   scripts/fork/check-modified-headers.sh [<rev>]   # default HEAD
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
. "$here/lib.sh"
rev=${1:-HEAD}

# First backticked token of each table row: a path, a glob (`*` also crosses
# `/`), or a directory ending in `/`.
listed=$(git show "$rev:MODIFICATIONS.md" 2>/dev/null | sed -nE 's/^\| *`([^`]+)`.*/\1/p' || true)

is_listed() {
  local f=$1 entry
  while IFS= read -r entry; do
    [ -z "$entry" ] && continue
    # shellcheck disable=SC2053 # unquoted on purpose: glob match
    [[ $f == $entry ]] && return 0
    case "$entry" in */) [[ $f == "$entry"* ]] && return 0 ;; esac
  done <<<"$listed"
  return 1
}

missing=()
while IFS=$'\t' read -r status path; do
  [ "$status" = M ] || continue
  is_listed "$path" && continue
  # sed reads the whole blob, so git show never dies of SIGPIPE under pipefail.
  top=$(git show "$rev:$path" | sed -n '1,5p')
  grep -qF -- "$FORK_HEADER" <<<"$top" && continue
  missing+=("$path")
done < <(git diff --no-renames --name-status "$FORK_BASELINE" "$rev")

if [ ${#missing[@]} -gt 0 ]; then
  echo "check-modified-headers: these modified upstream files have neither the header nor a MODIFICATIONS.md row (G8):" >&2
  printf '  %s\n' "${missing[@]}" >&2
  echo "Header (first line, or right after a shebang): <comment> $FORK_HEADER v0.8.1). See MODIFICATIONS.md." >&2
  exit 1
fi
echo "check-modified-headers: ok"
