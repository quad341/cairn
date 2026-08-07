#!/usr/bin/env bash
# Smoke test for recall_hitrate.py. Hermetic on purpose: it builds fixture
# HOMEs in a tmpdir rather than reading this machine's real cairn
# debug.jsonl, so it asserts exact numbers and passes on any box.
#
# Usage: ./test-recall-hitrate.sh        (exit 0 = pass)
set -uo pipefail

RH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/recall_hitrate.py"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/recall-hitrate-test.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

pass=0 fail=0
ok()   { pass=$((pass+1)); printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { fail=$((fail+1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; [ $# -gt 1 ] && printf '       %s\n' "$2"; }
is()   { [ "$2" = "$3" ] && ok "$1" || bad "$1" "want [$3] got [$2]"; }

line() { printf '%s\n' "$1" >>"$2"; }

# ---- fixtures ----
# identity "repo:cairn,branch:main" gets TWO prime_emit calls at different
# times. e3 is absent from the first's item_ids and present in the second's:
# a join against the wrong (non-nearest) prime_emit would silently
# misclassify e3@10:04 as recall-miss instead of surfaced.
mk_alpha() {
  local f="$TMP/alpha-claude/.local/state/cairn/debug.jsonl"
  mkdir -p "$(dirname "$f")"
  line '{"ts":"2026-08-01T10:00:00Z","kind":"prime_emit","invocation_id":"i1","command":"prime","identity_tags":["repo:cairn","branch:main"],"run_id":"r1","item_ids":["e1","e2"],"total_visible":2,"truncated_count":0}' "$f"
  line '{"ts":"2026-08-01T10:01:00Z","kind":"retrieval_outcome","invocation_id":"i2","command":"get","identity_tags":["repo:cairn","branch:main"],"run_id":"r1","outcome":"hit","entry_id":"e1","payload_tokens":40,"reuse_count":1}' "$f"
  line '{"ts":"2026-08-01T10:02:00Z","kind":"retrieval_outcome","invocation_id":"i3","command":"get","identity_tags":["repo:cairn","branch:main"],"run_id":"r1","outcome":"hit","entry_id":"e3","payload_tokens":40,"reuse_count":1}' "$f"
  line '{"ts":"2026-08-01T10:03:00Z","kind":"prime_emit","invocation_id":"i4","command":"prime","identity_tags":["repo:cairn","branch:main"],"run_id":"r2","item_ids":["e3","e4"],"total_visible":2,"truncated_count":0}' "$f"
  line '{"ts":"2026-08-01T10:04:00Z","kind":"retrieval_outcome","invocation_id":"i5","command":"get","identity_tags":["repo:cairn","branch:main"],"run_id":"r2","outcome":"stale","entry_id":"e3","payload_tokens":40,"reuse_count":2}' "$f"
  line '{"ts":"2026-08-01T10:05:00Z","kind":"retrieval_outcome","invocation_id":"i6","command":"get","identity_tags":["repo:cairn","branch:main"],"run_id":"r2","outcome":"miss","entry_id":"e9","payload_tokens":0,"reuse_count":0}' "$f"
  line '{"ts":"2026-08-01T10:06:00Z","kind":"retrieval_outcome","invocation_id":"i7","command":"get","identity_tags":["repo:beads","branch:main"],"run_id":"r3","outcome":"hit","entry_id":"e5","payload_tokens":40,"reuse_count":1}' "$f"
  # A line with no kind, and a malformed one: both must be skipped, not crash.
  line '{"ts":"2026-08-01T10:07:00Z","command":"noop"}' "$f"
  line 'not json at all' "$f"
}
mk_beta() {
  local f="$TMP/beta-claude/.local/state/cairn/debug.jsonl"
  mkdir -p "$(dirname "$f")"
  line '{"ts":"2026-08-01T11:00:00Z","kind":"prime_emit","invocation_id":"j1","command":"prime","identity_tags":["repo:tincan","branch:main"],"run_id":"r4","item_ids":["f1"],"total_visible":1,"truncated_count":0}' "$f"
  line '{"ts":"2026-08-01T11:01:00Z","kind":"retrieval_outcome","invocation_id":"j2","command":"get","identity_tags":["repo:tincan","branch:main"],"run_id":"r4","outcome":"hit","entry_id":"f1","payload_tokens":40,"reuse_count":1}' "$f"
}
mk_alpha
mk_beta
mkdir -p "$TMP/empty-claude/.local/state/cairn"   # exists, holds nothing

echo "recall_hitrate.py smoke test"

# ---- 1. join classification: nearest-preceding prime_emit, not just any ----
out="$("$RH" --json --homes "$TMP/alpha-claude" 2>/dev/null)"
is "surfaced (nearest-prime join)"  "$(jq -r '.surfaced'      <<<"$out")" "2"
is "recall-miss"                    "$(jq -r '.recall_miss'   <<<"$out")" "1"
is "unjoined (no preceding prime)"  "$(jq -r '.unjoined'      <<<"$out")" "1"
is "miss-excluded"                  "$(jq -r '.miss_excluded' <<<"$out")" "1"

# ---- 2. aggregate hit-rate and join coverage across homes ----
out="$("$RH" --json --homes "$TMP/alpha-claude" "$TMP/beta-claude" 2>/dev/null)"
is "surfaced across homes"    "$(jq -r '.surfaced'      <<<"$out")" "3"
is "recall-miss across homes" "$(jq -r '.recall_miss'   <<<"$out")" "1"
is "unjoined across homes"    "$(jq -r '.unjoined'      <<<"$out")" "1"
is "hit-rate = surfaced/(surfaced+recall_miss)" "$(jq -r '.hit_rate' <<<"$out")" "0.75"
is "join coverage = joined/(joined+unjoined)"   "$(jq -r '.join_coverage' <<<"$out")" "0.8"

# ---- 3. the JSON key set must not drift ----
is "json key set intact" "$(jq -r '[has("surfaced","recall_miss","unjoined","miss_excluded","hit_rate","join_coverage")]|all' <<<"$out")" "true"

# ---- 4. double-counting guard: a home named twice must not count twice ----
twice="$("$RH" --json --homes "$TMP/alpha-claude" "$TMP/alpha-claude/" 2>/dev/null | jq -r '.surfaced')"
is "aliased home counted once" "$twice" "2"

# ---- 5. stdout stays pure JSON even when a warning fires ----
sout="$("$RH" --json --homes "$TMP/alpha-claude" "$TMP/empty-claude" 2>"$TMP/err")"
jq -e 'type=="object"' <<<"$sout" >/dev/null 2>&1 \
  && ok "warning does not pollute stdout" \
  || bad "warning does not pollute stdout" "stdout was not parseable JSON"
grep -q 'empty-claude' "$TMP/err" \
  && ok "named-but-empty home warns" \
  || bad "named-but-empty home warns" "no warning on stderr for empty-claude"

# ---- 6. precedence: --homes > $RECALL_HITRATE_HOMES > discovery ----
env_only="$(RECALL_HITRATE_HOMES="$TMP/alpha-claude,$TMP/beta-claude" "$RH" --json 2>/dev/null | jq -r '.surfaced')"
is "env var honoured (comma form)" "$env_only" "3"
env_only="$(RECALL_HITRATE_HOMES="$TMP/alpha-claude:$TMP/beta-claude" "$RH" --json 2>/dev/null | jq -r '.surfaced')"
is "env var honoured (colon form)" "$env_only" "3"
flag_wins="$(RECALL_HITRATE_HOMES="$TMP/beta-claude" "$RH" --json --homes "$TMP/alpha-claude" 2>/dev/null | jq -r '.surfaced')"
is "--homes overrides env var" "$flag_wins" "2"

# ---- 7. discovery is by convention (siblings of $HOME), not hardcoded ----
disc="$(HOME="$TMP/alpha-claude" "$RH" --json 2>&1 >/dev/null | grep -c 'beta-claude')"
is "discovers sibling *-claude" "$disc" "1"
is "no path pinned to one machine" "$(grep -c '/home/jaword' "$RH")" "0"

# ---- 8. XDG_STATE_HOME overrides the fallback for the current process's own home ----
xdgtmp="$TMP/xdg-target"
mkdir -p "$xdgtmp/cairn"
cp "$TMP/alpha-claude/.local/state/cairn/debug.jsonl" "$xdgtmp/cairn/debug.jsonl"
xdg_out="$(HOME="$TMP/alpha-claude" XDG_STATE_HOME="$xdgtmp" "$RH" --json --homes "$TMP/alpha-claude" 2>/dev/null | jq -r '.surfaced')"
is "XDG_STATE_HOME override honoured" "$xdg_out" "2"

# ---- 9. failure modes exit non-zero rather than printing an empty report ----
mkdir -p "$TMP/isolated/deep"
HOME="$TMP/isolated/deep" "$RH" --json >/dev/null 2>&1
is "no homes found -> rc 1" "$?" "1"
"$RH" --json --homes "$TMP/empty-claude" >/dev/null 2>&1
is "no records found -> rc 1" "$?" "1"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
