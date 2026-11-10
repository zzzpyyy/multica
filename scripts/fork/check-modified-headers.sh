#!/usr/bin/env bash
# G8: every upstream file the fork modified says so (status M against the
# baseline). A file that can hold a comment needs the header line in its first
# 5 lines; only regenerated files ($FORK_REGENERATED) are excused, and they
# need a MODIFICATIONS.md row instead. A file that cannot hold a comment
# (JSON, images, ...) needs a MODIFICATIONS.md row.
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

commentable() {
  case "$1" in
    *.go | *.ts | *.tsx | *.js | *.jsx | *.mjs | *.cjs | *.mts | *.sql | *.yml | *.yaml | *.sh | *.ps1 | *.md | *.css | *.html | *.toml) return 0 ;;
  esac
  return 1
}

regenerated() {
  local entry
  for entry in $FORK_REGENERATED; do
    [[ $1 == "$entry" || ($entry == */ && $1 == "$entry"*) ]] && return 0
  done
  return 1
}

missing=()
while IFS=$'\t' read -r status path; do
  [ "$status" = M ] || continue
  if commentable "$path" && ! regenerated "$path"; then
    # sed reads the whole blob, so git show never dies of SIGPIPE under pipefail.
    top=$(git show "$rev:$path" | sed -n '1,5p')
    grep -qF -- "$FORK_HEADER" <<<"$top" && continue
    missing+=("$path (needs the header)")
    continue
  fi
  is_listed "$path" && continue
  missing+=("$path (needs a MODIFICATIONS.md row)")
done < <(git diff --no-renames --name-status "$FORK_BASELINE" "$rev")

if [ ${#missing[@]} -gt 0 ]; then
  echo "check-modified-headers: modified upstream files not marked as the fork's (G8):" >&2
  printf '  %s\n' "${missing[@]}" >&2
  echo "Header (first line, or right after a shebang): <comment> $FORK_HEADER v0.8.1). See MODIFICATIONS.md." >&2
  exit 1
fi
echo "check-modified-headers: ok"
