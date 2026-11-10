# Shared constants for the fork's checks. Sourced by the scripts next to it.

FORK_REPO="zzzpyyy/multica"
# ponytail: one baseline. Bump it in the same commit that merges a new upstream
# release, so upstream's commits stay outside every "what did we add" range.
FORK_BASELINE="${FORK_BASELINE:-b1ea927def6490c43bedff537b6f4ed9a47fa828}"
FORK_EMAIL="${FORK_EMAIL:-167217540+zzzpyyy@users.noreply.github.com}"
# Regenerated on every build, so a header comment would be overwritten (G8):
# space-separated paths, a trailing / for a directory.
FORK_REGENERATED="${FORK_REGENERATED:-server/pkg/db/generated/ pnpm-lock.yaml}"
# Prefix of the one-line header every modified upstream file carries (G8).
FORK_HEADER="Modified in $FORK_REPO (fork of multica-ai/multica"

# G7 patterns, ERE, matched case-insensitively. Each is written so that this
# file does not match itself (e.g. `[.]` instead of a literal dot).
#   domains/IPs: may also appear in upstream's own installers (Chromium ships
#                search-engine and network tables), so artifact scans excuse
#                hits that the baseline installer has at the same path.
#   secrets:     server addresses and tokens; never excused.
FORK_DOMAIN_IP_RE='baidu[-]int|baidu[.]com|bcc[-]bddx|baidu[b]ce|(^|[^0-9.])(10([.][0-9]{1,3}){3}|172[.](1[6-9]|2[0-9]|3[01])([.][0-9]{1,3}){2}|192[.]168([.][0-9]{1,3}){2})([^0-9]|$)'
FORK_SECRET_RE='mul_[A-Za-z0-9]{20,}|mat_[A-Za-z0-9]{20,}'

# The self-hosted server's address must not be in the repo either, so it
# lives outside it: one ERE per line in this file (blank lines and # ignored).
FORK_PATTERNS_FILE="${FORK_PATTERNS_FILE:-$HOME/.config/multica-fork/public-patterns}"
if [ -f "$FORK_PATTERNS_FILE" ]; then
  while IFS= read -r line; do
    case "$line" in '' | '#'*) continue ;; esac
    FORK_SECRET_RE="$FORK_SECRET_RE|$line"
  done <"$FORK_PATTERNS_FILE"
fi

# Commits we added on top of upstream: reachable from $1, not from the
# baseline, not from any upstream remote-tracking ref.
fork_rev_args() {
  printf '%s\n' "$1" --not "$FORK_BASELINE" --remotes=upstream
}
