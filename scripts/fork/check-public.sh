#!/usr/bin/env bash
# G7: nothing internal goes public. Scans what the fork added on top of
# upstream (every commit message and every added line of every commit,
# including lines a later commit removed), or unpacked release installers.
#
#   scripts/fork/check-public.sh [<rev>]          # default HEAD
#   scripts/fork/check-public.sh --artifacts <dir> [--baseline-artifacts <dir>]
#
# Artifact mode unpacks .zip/.tar.gz/.tgz/.dmg under <dir>. Domain/IP hits that
# the baseline (upstream) installers have at the same path are excused; server
# addresses and tokens never are.
set -euo pipefail
export LC_ALL=C
here=$(cd "$(dirname "$0")" && pwd)
. "$here/lib.sh"

scan_commits() {
  local rev=$1 args
  mapfile -t args < <(fork_rev_args "$rev")
  # One "<sha>:<where>: <text>" line per added line or message line.
  {
    git log --no-color --format='commit %H' -p "${args[@]}" | awk '
      /^commit [0-9a-f]{40}$/ { c = substr($2, 1, 12); next }
      /^\+\+\+ / { f = substr($0, 5); next }
      /^\+/ { print c ":" f ": " substr($0, 2) }'
    git log --no-color --format='commit %H%n%B' "${args[@]}" | awk '
      /^commit [0-9a-f]{40}$/ { c = substr($2, 1, 12); next }
      { print c ":(message): " $0 }'
  } | grep -i -E -- "$FORK_DOMAIN_IP_RE|$FORK_SECRET_RE" || true
}

unpack() { # <src dir> <dest dir>
  local src=$1 dest=$2 f rel out mnt
  while IFS= read -r -d '' f; do
    rel=${f#"$src"/}
    out="$dest/$rel.d"
    case "$f" in
      *.zip) mkdir -p "$out" && unzip -qq -o "$f" -d "$out" ;;
      *.tar.gz | *.tgz) mkdir -p "$out" && tar -xzf "$f" -C "$out" ;;
      *.dmg)
        mkdir -p "$out"
        if command -v hdiutil >/dev/null; then
          mnt=$(mktemp -d)
          hdiutil attach -nobrowse -readonly -mountpoint "$mnt" "$f" >/dev/null
          cp -R "$mnt"/. "$out"/
          hdiutil detach "$mnt" >/dev/null
        elif command -v 7z >/dev/null; then
          7z x -y -o"$out" "$f" >/dev/null
        else
          echo "check-public: cannot unpack $rel (needs hdiutil or 7z)" >&2
          return 1
        fi
        ;;
      *) mkdir -p "$(dirname "$dest/$rel")" && cp "$f" "$dest/$rel" ;;
    esac
  done < <(find "$src" -type f -print0)
}

# "<path inside the app or archive>\t<match>" for every hit of $2 under $1.
# The path drops archive names and versions so ours and upstream's line up.
artifact_hits() {
  { grep -a -r -o -i -H -E -- "$2" "$1" 2>/dev/null || true; } | awk -v n=$((${#1} + 2)) '{
    i = index($0, ":"); p = substr($0, n, i - n); m = substr($0, i + 1)
    j = index(p, ".app/"); if (j) p = substr(p, j + 5); else sub(/^[^\/]*\//, "", p)
    print p "\t" m }' | sort -u
}

scan_artifacts() {
  local dir=$1 base=${2:-} tmp hits
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' RETURN
  unpack "$dir" "$tmp/ours"
  hits=$(artifact_hits "$tmp/ours" "$FORK_SECRET_RE")
  local domain
  domain=$(artifact_hits "$tmp/ours" "$FORK_DOMAIN_IP_RE")
  if [ -n "$domain" ] && [ -n "$base" ]; then
    unpack "$base" "$tmp/base"
    domain=$(comm -23 <(printf '%s\n' "$domain") <(artifact_hits "$tmp/base" "$FORK_DOMAIN_IP_RE"))
  fi
  printf '%s\n%s\n' "$hits" "$domain" | sed '/^$/d'
}

if [ "${1:-}" = "--artifacts" ]; then
  [ -d "${2:-}" ] || { echo "usage: $0 --artifacts <dir> [--baseline-artifacts <dir>]" >&2; exit 2; }
  base=""
  [ "${3:-}" = "--baseline-artifacts" ] && base=${4:?missing baseline dir}
  found=$(scan_artifacts "$2" "$base")
else
  found=$(scan_commits "${1:-HEAD}")
fi

if [ -n "$found" ]; then
  echo "check-public: internal data found (G7). Remove it; for commits that means rewriting them before they are pushed:" >&2
  printf '%s\n' "$found" | head -50 >&2
  exit 1
fi
echo "check-public: ok"
