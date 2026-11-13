#!/usr/bin/env bash
# Self-test for scripts/fork: every rule must fail when broken and pass when not.
#   bash scripts/fork/check.test.sh
set -euo pipefail
src=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
export FORK_EMAIL=dev@example.test
export FORK_PATTERNS_FILE="$tmp/patterns"
export FORK_REGENERATED="gen/"
printf '# local only\nsecret-host[.]example\n' >"$FORK_PATTERNS_FILE"

# Built at runtime so this file itself stays clean.
dom="baidu"; dom="intra.$dom-int.com"
ip="10.$((1 + 1)).3.4"
tok="mul_$(printf 'a%.0s' $(seq 1 24))"

pass=0
ok() { if "$@" >/dev/null 2>&1; then pass=$((pass + 1)); else echo "FAIL (expected ok): $*"; exit 1; fi; }
no() { if "$@" >/dev/null 2>&1; then echo "FAIL (expected failure): $*"; exit 1; else pass=$((pass + 1)); fi; }

repo="$tmp/repo"
git init -q "$repo" && cd "$repo"
git config user.name dev && git config user.email "$FORK_EMAIL"
mkdir -p scripts/fork gen && cp -R "$src"/. scripts/fork/
mkdir -p loc/en && printf '{}\n' >loc/en/a.json
printf 'package a\n' >a.go && printf '{}\n' >data.json && printf 'x\n' >gen/q.go
git add -A && git commit -qm base
export FORK_BASELINE=$(git rev-parse HEAD)
pub=scripts/fork/check-public.sh hdr=scripts/fork/check-modified-headers.sh

# --- check-public: commits ---
echo clean >b.txt && git add b.txt && git commit -qm clean
ok $pub
echo "host $dom" >c.txt && git add c.txt && git commit -qm leak
git rm -q c.txt && git commit -qm "remove it again"
no $pub # still in history
git reset -q --hard HEAD~2
git commit -q --allow-empty -m "see $ip"
no $pub # commit message
git reset -q --hard HEAD~1
echo "url=https://secret-host.example:8006" >d.txt && git add d.txt && git commit -qm d
no $pub # local server pattern
git reset -q --hard HEAD~1
echo "token=$tok" >d.txt && git add d.txt && git commit -qm d
no $pub
git reset -q --hard HEAD~1
echo "v1.10.2.3.4 and 10.0.x" >e.txt && git add e.txt && git commit -qm e
ok $pub # not an IPv4 literal

# --- check-modified-headers ---
printf 'package a\nfunc f() {}\n' >a.go && git commit -qam a
no $hdr
printf '// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.\npackage a\nfunc f() {}\n' >a.go
git commit -qam a2
ok $hdr
printf '{"a":1}\n' >data.json && printf 'y\n' >gen/q.go && git commit -qam json
no $hdr
printf '| 文件 | 目的 |\n|---|---|\n| `data.json` | x |\n| `gen/` | sqlc |\n' >MODIFICATIONS.md && git add MODIFICATIONS.md && git commit -qm mods
ok $hdr
printf '{"k":1}\n' >loc/en/a.json && git commit -qam loc
no $hdr
printf '| `loc/*/a.json` | i18n |\n' >>MODIFICATIONS.md && git commit -qam glob
ok $hdr
# A file that can hold a comment needs the header; a row does not excuse it.
printf 'package a\nfunc g() {}\n' >a.go && printf '| `a.go` | x |\n' >>MODIFICATIONS.md && git commit -qam "row only"
no $hdr
printf '// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.\npackage a\nfunc g() {}\n' >a.go && git commit -qam a3
ok $hdr

# --- pre-push identity ---
hook=scripts/fork/git-hooks/pre-push
z=0000000000000000000000000000000000000000
ok sh -c "echo 'refs/heads/f $(git rev-parse HEAD) refs/heads/f $z' | bash $hook"
git -c user.email=me@corp.test commit -q --allow-empty -m other
no sh -c "echo 'refs/heads/f $(git rev-parse HEAD) refs/heads/f $z' | bash $hook"
ok sh -c "echo 'refs/heads/f $z refs/heads/f $z' | bash $hook" # deletion
git reset -q --hard HEAD~1
printf '{"k":2}\n' >loc/en/b.json && git add loc && git commit -qm "new file is fine"
ok sh -c "echo 'refs/heads/f $(git rev-parse HEAD) refs/heads/f $z' | bash $hook"
printf 'package a\nfunc g() {}\n' >a.go && git commit -qam "drop header"
no sh -c "echo 'refs/heads/f $(git rev-parse HEAD) refs/heads/f $z' | bash $hook" # G8

# --- check-public: artifacts ---
mk() { # <dir> <archive> <content>
  mkdir -p "$1/stage/Multica.app/Contents" && printf '%s\n' "$3" >"$1/stage/Multica.app/Contents/app.js"
  (cd "$1/stage" && zip -qr "../$2" Multica.app) && rm -rf "$1/stage"
}
mk "$tmp/ours" Multica-1.0.0-fork.1-mac.zip "x $ip y"
mk "$tmp/base" Multica-1.0.0-mac.zip "x $ip y"
mk "$tmp/tok" Multica-1.0.0-fork.1-mac.zip "x $ip $tok"
mk "$tmp/clean" Multica-1.0.0-fork.1-mac.zip "nothing here"
ok $pub --artifacts "$tmp/clean"
no $pub --artifacts "$tmp/ours"
ok $pub --artifacts "$tmp/ours" --baseline-artifacts "$tmp/base" # same hit upstream
no $pub --artifacts "$tmp/tok" --baseline-artifacts "$tmp/base"   # tokens never excused
# A hit inside a binary, between NUL bytes (as in Electron Framework): the
# same hit upstream must still excuse it.
mkbin() { # <dir> <archive>
  mkdir -p "$1/stage/Multica.app/Contents" && printf 'x\0%s\0y\0%s\0' "$ip" "${ip%.*}.9" >"$1/stage/Multica.app/Contents/app.bin"
  (cd "$1/stage" && zip -qr "../$2" Multica.app) && rm -rf "$1/stage"
}
mkbin "$tmp/oursbin" Multica-1.0.0-fork.1-mac.zip
mkbin "$tmp/basebin" Multica-1.0.0-mac.zip
no $pub --artifacts "$tmp/oursbin"
ok $pub --artifacts "$tmp/oursbin" --baseline-artifacts "$tmp/basebin"

echo "scripts/fork: $pass checks passed"
