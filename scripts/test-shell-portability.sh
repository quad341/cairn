#!/usr/bin/env bash
#
# test-shell-portability.sh — guards the "sourced into the CALLER's shell"
# contract of scripts/rebase-resolve-lib.sh. (bead crn-k1hw / port crn-sns6)
#
# PORTED COPY. This is an independent copy of
# gc-management's packs/actual/deployer/tests/test-shell-portability.sh
# (the immediate upstream scripts/rebase-resolve-lib.sh in THIS repo was
# ported from — see that file's own header), matching the "copied, not
# shared" convention documented there: do NOT hand-edit this copy to "sync"
# with the gc-management original — port forward from there instead. Adapted
# only for this repo's flat layout (script and lib both live in scripts/,
# not tests/ + scripts/); the test logic itself is unchanged.
#
# WHY THIS FILE EXISTS
#
# rebase-resolve-lib.sh is never executed; every caller SOURCES it
# (`. rebase-resolve-lib.sh`). Sourcing does not start a new interpreter, so
# the `#!/usr/bin/env bash` line at its top is inert — the library's code runs
# in whatever shell the caller already is. zsh is a live, confirmed caller
# shell in this fleet's worktrees.
#
# That makes bash-only syntax in the library a live production fault, and a
# uniquely nasty one. Measured under zsh 5.9 with the pre-fix `${path,,}`
# restored:
#
#   * `zsh -n` (parse check) exits 0 — a syntax check CANNOT catch it.
#   * Merely sourcing the file is fine. The fault fires only when the
#     function is CALLED, so a test that just sources the lib passes.
#   * When it does fire, zsh treats "bad substitution" as FATAL: it tears
#     down the whole calling script, so the caller never reaches the
#     route-to-the-builder branch it would have taken on an ordinary failure.
#   * The torn-down caller exits 0. The failure is therefore not merely
#     fail-dead, it is falsely-green to anything that checks status.
#
# A SECOND, INDEPENDENT defect in the same class was found while fixing the
# first: zsh ties the scalar $PATH to an array named `path`, so the library's
# `local path="$1"` blanked the command search path for the body of both
# classifier functions. Under zsh that made every external command inside
# them (tr, mktemp, awk, mv, rm) fail "command not found" —
# resolve_conflict_markers_in_file had therefore NEVER worked in a zsh caller,
# returning rc=2 ("IO error") and silently routing every conflict to the
# builder instead of auto-resolving it. That one fails safe rather than dead,
# which is exactly why it went unnoticed. Layer 1 below guards both defects.
#
# LAYERS
#   1. Static scan (always runs) — the library, with full-line comments
#      stripped, must contain no bash-only constructs and no local named
#      after a zsh special parameter. Includes self-tests proving the scanner
#      actually fires on known-bad input, so a scanner that silently matches
#      nothing cannot pass everything.
#   2. zsh parity (needs zsh) — is_additive_keepboth_path must return
#      IDENTICAL verdicts under bash and zsh across a path table chosen to
#      exercise the case-folding specifically.
#   3. zsh liveness (needs zsh) — after calling into the library, the caller
#      must still reach its next statement. Direct regression test for the
#      fail-dead behavior above.
#   4. zsh end-to-end (needs zsh + git) — attempt_trivial_conflict_resolution
#      driven against a real conflicted temp repo, under zsh.
#   5. zsh branch-safety (needs zsh) — the shared-worktree branch classifier
#      gates every push target, so its verdict must not drift between shells.
#
# Layers 2-5 SKIP (not fail) when zsh is absent; layer 1 is the backstop.

set -uo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$TEST_DIR/rebase-resolve-lib.sh"

pass=0; fail=0; skip=0
record_pass() { echo "  ok   $1"; pass=$((pass + 1)); }
record_fail() { echo "  FAIL $1 — $2"; fail=$((fail + 1)); }
record_skip() { echo "  skip $1 — $2"; skip=$((skip + 1)); }

have_zsh() { command -v zsh >/dev/null 2>&1; }

# ---------------------------------------------------------------------------
# Layer 1 — static scan
# ---------------------------------------------------------------------------

# Constructs that are fatal or silently wrong when this library is sourced
# into a zsh caller. Each entry is "<ERE>\t<human explanation>".
#
# ${var,,} and `local path` are the crn-k1hw regressions proper. The others
# are neighbours in the same class: constructs a future editor might reach
# for that would reintroduce exactly this failure mode.
bashism_patterns() {
    cat <<'PATTERNS'
\$\{[A-Za-z_][A-Za-z_0-9]*,,?\}	${var,,} / ${var,} lowercasing (bash-4 only; fatal under zsh — use `tr`)
\$\{[A-Za-z_][A-Za-z_0-9]*\^\^?\}	${var^^} / ${var^} uppercasing (bash-4 only; fatal under zsh — use `tr`)
(^|[^[:alnum:]_])(declare|local)[[:space:]]+-A	associative arrays (declare/local -A; not portable to zsh as written)
(^|[^[:alnum:]_])(declare|local)[[:space:]]+-n	namerefs (declare/local -n; bash 4.3+ only)
(^|[^[:alnum:]_])(mapfile|readarray)([[:space:]]|$)	mapfile/readarray (bash only; use a read loop)
\$\{![A-Za-z_]	${!var} indirect expansion (bash only)
(^|[^[:alnum:]_])BASH_REMATCH	BASH_REMATCH (bash only; zsh populates $MATCH/$match)
(^|[^[:alnum:]_])BASH_SOURCE	BASH_SOURCE (unset in a zsh caller — cannot be used for path resolution)
(^|[^[:alnum:]_])shopt([[:space:]]|$)	shopt (bash only)
(^|[^[:alnum:]_])(local|typeset|declare)[[:space:]]+(path|status|argv|cdpath|fpath|manpath|module_path|infopath|prompt|psvar|watch|signals|options|commands|functions|aliases|parameters|dirstack|fignore|mailpath)([[:space:]]*=|[[:space:]]|$)	local named after a zsh SPECIAL PARAMETER — under zsh this shadows the real one (`local path` blanks $PATH for the whole function, so every external command inside it fails "command not found"). Rename it, e.g. path -> file_path
PATTERNS
}

# Strip FULL-LINE comments only.
#
# The library's own comments deliberately name ${var,,} and `local path` to
# explain why they are banned, so an unstripped scan would false-positive on
# the fix's own documentation. Stripping is intentionally conservative —
# trailing comments are NOT stripped, because the failure direction matters:
# an unstripped trailing comment can only cause a false POSITIVE (a loud,
# easily-corrected test failure), whereas over-aggressive stripping risks a
# false NEGATIVE, which is the outcome this whole file exists to prevent.
strip_full_line_comments() {
    sed -e 's/^[[:space:]]*#.*$//' "$1"
}

# scan_for_bashisms <file> — prints one "<line>: <explanation>" per hit.
scan_for_bashisms() {
    local file="$1" stripped
    stripped="$(mktemp "${TMPDIR:-/tmp}/gc-portability-scan.XXXXXX")" || return 2
    strip_full_line_comments "$file" > "$stripped"

    local pattern explanation
    while IFS="$(printf '\t')" read -r pattern explanation; do
        [ -n "$pattern" ] || continue
        grep -nE "$pattern" "$stripped" 2>/dev/null | while IFS= read -r hit; do
            printf '%s: %s\n' "${hit%%:*}" "$explanation"
        done
    done < <(bashism_patterns)

    rm -f "$stripped"
}

test_static_no_bashisms() {
    local hits
    hits="$(scan_for_bashisms "$LIB")"
    if [ -z "$hits" ]; then
        record_pass "static: library contains no zsh-hostile constructs"
    else
        record_fail "static: library contains no zsh-hostile constructs" \
            "hits (line: reason):
$hits"
    fi
}

# The scanner is only worth anything if it actually fires. A typo in a
# pattern would otherwise make layer 1 pass unconditionally forever.
test_static_scanner_has_teeth() {
    local bad rc_hits
    bad="$(mktemp "${TMPDIR:-/tmp}/gc-portability-bad.XXXXXX")" || return
    # Reconstruct the exact pre-fix shape from crn-k1hw — it carries BOTH
    # defects: the bash-only lowercasing and the $PATH-shadowing local.
    {
        printf '%s\n' 'is_additive_keepboth_path() {'
        printf '%s\n' '    local path="$1"'
        printf '%s\n' '    local lower="${path,,}"'
        printf '%s\n' '    local base="${path##*/}"'
        printf '%s\n' '    local lbase="${base,,}"'
        printf '%s\n' '    return 1'
        printf '%s\n' '}'
    } > "$bad"

    rc_hits="$(scan_for_bashisms "$bad")"
    if printf '%s' "$rc_hits" | grep -q 'lowercasing'; then
        record_pass "static: scanner detects the known-bad pre-fix construct"
    else
        record_fail "static: scanner detects the known-bad pre-fix construct" \
            "scanner returned no lowercasing hit for a file containing \${path,,} — patterns are broken"
    fi

    if printf '%s' "$rc_hits" | grep -q 'SPECIAL PARAMETER'; then
        record_pass "static: scanner detects a local named after a zsh special parameter"
    else
        record_fail "static: scanner detects a local named after a zsh special parameter" \
            "scanner returned no special-parameter hit for a file containing 'local path=' — patterns are broken"
    fi
    rm -f "$bad"
}

# The scanner must not fire on the fix's own explanatory comments — otherwise
# maintainers learn to ignore it.
test_static_scanner_ignores_comments() {
    local commented hits
    commented="$(mktemp "${TMPDIR:-/tmp}/gc-portability-comment.XXXXXX")" || return
    {
        printf '%s\n' '# Uses `tr`, NOT bash-4 ${var,,}: this file is SOURCED.'
        printf '%s\n' '    # NOT `local path` — zsh ties that name to $PATH.'
        printf '%s\n' 'f() { printf %s "$1" | LC_ALL=C tr "A-Z" "a-z"; }'
    } > "$commented"

    hits="$(scan_for_bashisms "$commented")"
    if [ -z "$hits" ]; then
        record_pass "static: scanner ignores full-line comments"
    else
        record_fail "static: scanner ignores full-line comments" \
            "false positive on comment-only mentions:
$hits"
    fi
    rm -f "$commented"
}

# ---------------------------------------------------------------------------
# Layers 2-5 — live zsh
# ---------------------------------------------------------------------------

# Path table for the parity check. Every UPPERCASE entry exists specifically
# to exercise the case-folding: with the pre-fix code these abort the shell,
# and with a fix that dropped the lowercasing they would misclassify (return
# 1 where 0 is required). Format: "<path>\t<expected rc>".
parity_paths() {
    cat <<'PATHS'
README.md	0
Docs/Guide.TXT	0
DOCS/deploy.go	0
CHANGELOG	0
News/2026-01.txt	0
Tests/Foo.go	0
TESTDATA/blob.bin	0
Fixtures/sample.json	0
x/__Snapshots__/a.snap	0
pkg/Handler_test.go	0
Spec/thing.rb	0
Test-Foo.SH	0
docs/nested/deep/x.adoc	0
src/Main.go	1
internal/Server.java	1
cmd/Tool.c	1
Makefile	1
src/lib.rs	1
PATHS
}

# Emit a driver that sources the lib, calls the classifier over the table,
# and prints "<path>=<rc>" for each. Run under both shells and diff.
write_parity_driver() {
    local out="$1"
    {
        printf '%s\n' ". \"\$1\""
        printf '%s\n' 'while IFS="	" read -r p _expected; do'
        printf '%s\n' '    [ -n "$p" ] || continue'
        printf '%s\n' '    if is_additive_keepboth_path "$p"; then rc=0; else rc=1; fi'
        printf '%s\n' '    printf "%s=%s\n" "$p" "$rc"'
        printf '%s\n' 'done < "$2"'
    } > "$out"
}

test_zsh_parity() {
    if ! have_zsh; then
        record_skip "zsh parity: identical verdicts under bash and zsh" "zsh not installed"
        return
    fi

    local driver table bash_out zsh_out
    driver="$(mktemp "${TMPDIR:-/tmp}/gc-parity-driver.XXXXXX")"
    table="$(mktemp "${TMPDIR:-/tmp}/gc-parity-table.XXXXXX")"
    write_parity_driver "$driver"
    parity_paths > "$table"

    bash_out="$(bash "$driver" "$LIB" "$table" 2>&1)"
    zsh_out="$(zsh "$driver" "$LIB" "$table" 2>&1)"

    if [ "$bash_out" = "$zsh_out" ]; then
        record_pass "zsh parity: identical verdicts under bash and zsh"
    else
        record_fail "zsh parity: identical verdicts under bash and zsh" \
            "bash and zsh disagree.
--- bash ---
$bash_out
--- zsh ---
$zsh_out"
    fi

    # Independently assert the verdicts are actually CORRECT under zsh, not
    # merely equal to bash's. (Both shells agreeing on a wrong answer would
    # sail through the parity check above.)
    local mismatches=""
    while IFS="$(printf '\t')" read -r p expected; do
        [ -n "$p" ] || continue
        local got
        got="$(printf '%s' "$zsh_out" | grep -F "$p=" | head -1 | sed 's/.*=//')"
        if [ "$got" != "$expected" ]; then
            mismatches="$mismatches
  $p: expected $expected, got ${got:-<no output>}"
        fi
    done < "$table"

    if [ -z "$mismatches" ]; then
        record_pass "zsh parity: verdicts match the expected classification"
    else
        record_fail "zsh parity: verdicts match the expected classification" \
            "under zsh:$mismatches"
    fi

    rm -f "$driver" "$table"
}

# The core regression test. Under the pre-fix library this prints nothing and
# still exits 0 — so we assert on the SENTINEL, never on the exit code.
test_zsh_caller_survives_the_call() {
    if ! have_zsh; then
        record_skip "zsh liveness: caller reaches its routing path" "zsh not installed"
        return
    fi

    local out
    out="$(zsh -c '
        . "$1"
        is_additive_keepboth_path "README.md" >/dev/null 2>&1
        printf "REACHED_ROUTING_PATH\n"
    ' zsh-portability-probe "$LIB" 2>/dev/null)"

    if [ "$out" = "REACHED_ROUTING_PATH" ]; then
        record_pass "zsh liveness: caller reaches its routing path after the call"
    else
        record_fail "zsh liveness: caller reaches its routing path after the call" \
            "the calling script was torn down mid-run (a bash-only expansion is fatal under zsh). Got: [${out}]"
    fi
}

test_zsh_end_to_end_resolution() {
    if ! have_zsh; then
        record_skip "zsh end-to-end: trivial conflict resolves under zsh" "zsh not installed"
        return
    fi
    if ! command -v git >/dev/null 2>&1; then
        record_skip "zsh end-to-end: trivial conflict resolves under zsh" "git not installed"
        return
    fi

    local repo
    repo="$(mktemp -d "${TMPDIR:-/tmp}/gc-portability-repo.XXXXXX")"
    (
        export GIT_AUTHOR_NAME="Test Author" GIT_AUTHOR_EMAIL="author@example.com"
        export GIT_COMMITTER_NAME="Test Deployer" GIT_COMMITTER_EMAIL="deployer@example.com"
        export GIT_CONFIG_NOSYSTEM=1
        unset GIT_DIR GIT_WORK_TREE
        git -C "$repo" init -q -b main
        git -C "$repo" config commit.gpgsign false
        mkdir -p "$repo/docs"

        # A doc file: both sides append different lines. This is the
        # ADDITIVE-BOTH-on-an-allowlisted-path shape, which is reachable ONLY
        # through is_additive_keepboth_path — precisely the fixed function.
        printf 'base\n' > "$repo/docs/NOTES.md"
        git -C "$repo" add -A && git -C "$repo" commit -qm base

        git -C "$repo" checkout -q -b feature
        printf 'base\nfrom-feature\n' > "$repo/docs/NOTES.md"
        git -C "$repo" commit -qam feature

        git -C "$repo" checkout -q main
        printf 'base\nfrom-main\n' > "$repo/docs/NOTES.md"
        git -C "$repo" commit -qam main

        git -C "$repo" checkout -q feature
        git -C "$repo" rebase main >/dev/null 2>&1 || true
    ) >/dev/null 2>&1

    # Confirm the fixture really is mid-conflict; otherwise the assertion
    # below would pass vacuously.
    if ! git -C "$repo" ls-files --unmerged 2>/dev/null | grep -q .; then
        record_fail "zsh end-to-end: trivial conflict resolves under zsh" \
            "fixture did not produce a conflicted tree — test would pass vacuously"
        rm -rf "$repo"
        return
    fi

    local out
    out="$(cd "$repo" && zsh -c '
        . "$1"
        if attempt_trivial_conflict_resolution; then
            printf "RESOLVED\n"
        else
            printf "ROUTED\n"
        fi
    ' zsh-portability-probe "$LIB" 2>/dev/null)"

    if [ "$out" = "RESOLVED" ]; then
        if grep -q 'from-feature' "$repo/docs/NOTES.md" && grep -q 'from-main' "$repo/docs/NOTES.md"; then
            record_pass "zsh end-to-end: additive doc conflict keeps both sides under zsh"
        else
            record_fail "zsh end-to-end: additive doc conflict keeps both sides under zsh" \
                "resolved, but the merged file lost a side: $(tr '\n' '|' < "$repo/docs/NOTES.md")"
        fi
    elif [ "$out" = "ROUTED" ]; then
        record_fail "zsh end-to-end: additive doc conflict keeps both sides under zsh" \
            "library refused an allowlisted additive conflict under zsh (classifier likely misbehaving)"
    else
        record_fail "zsh end-to-end: additive doc conflict keeps both sides under zsh" \
            "the calling script was torn down mid-run under zsh. Got: [${out}]"
    fi

    rm -rf "$repo"
}

# is_shared_worktree_branch gates every push target (assert_safe_push_target
# refuses to push a shared gc-<agent>-<12hex> worktree branch), so a verdict
# that drifts between shells is a branch-safety bug, not a cosmetic one. It
# also uses `[[ =~ ]]` with an interval quantifier — the one remaining
# construct whose cross-shell behavior is worth pinning rather than assuming.
test_zsh_shared_branch_classifier_parity() {
    if ! have_zsh; then
        record_skip "zsh branch-safety: shared-branch verdicts agree across shells" "zsh not installed"
        return
    fi

    local driver table bash_out zsh_out
    driver="$(mktemp "${TMPDIR:-/tmp}/gc-branch-driver.XXXXXX")"
    table="$(mktemp "${TMPDIR:-/tmp}/gc-branch-table.XXXXXX")"
    cat > "$table" <<'BRANCHES'
gc-deployer-0123456789ab	0
gc-pack-author-abcdef012345	0
gc-mpr-review-bot-0011aabbccdd	0
deploy/crn-k1hw-0123456789ab	1
release/1.2.3	1
main	1
gc-deployer-0123456789ABCDEF	1
gc-deployer-short	1
BRANCHES
    {
        printf '%s\n' ". \"\$1\""
        printf '%s\n' 'while IFS="	" read -r b _expected; do'
        printf '%s\n' '    [ -n "$b" ] || continue'
        printf '%s\n' '    if is_shared_worktree_branch "$b"; then rc=0; else rc=1; fi'
        printf '%s\n' '    printf "%s=%s\n" "$b" "$rc"'
        printf '%s\n' 'done < "$2"'
    } > "$driver"

    bash_out="$(bash "$driver" "$LIB" "$table" 2>&1)"
    zsh_out="$(zsh "$driver" "$LIB" "$table" 2>&1)"

    if [ "$bash_out" = "$zsh_out" ]; then
        record_pass "zsh branch-safety: shared-branch verdicts agree across shells"
    else
        record_fail "zsh branch-safety: shared-branch verdicts agree across shells" \
            "bash and zsh disagree — a push-target guard that differs by shell.
--- bash ---
$bash_out
--- zsh ---
$zsh_out"
    fi

    local mismatches=""
    while IFS="$(printf '\t')" read -r b expected; do
        [ -n "$b" ] || continue
        local got
        got="$(printf '%s' "$zsh_out" | grep -F "$b=" | head -1 | sed 's/.*=//')"
        if [ "$got" != "$expected" ]; then
            mismatches="$mismatches
  $b: expected $expected, got ${got:-<no output>}"
        fi
    done < "$table"

    if [ -z "$mismatches" ]; then
        record_pass "zsh branch-safety: verdicts match the expected classification"
    else
        record_fail "zsh branch-safety: verdicts match the expected classification" \
            "under zsh:$mismatches"
    fi

    rm -f "$driver" "$table"
}

# ---------------------------------------------------------------------------

run_all() {
    echo "shell-portability: $LIB"
    if have_zsh; then
        echo "  (zsh: $(zsh --version 2>&1 | head -1))"
    else
        echo "  (zsh: NOT INSTALLED — live layers will skip, static layer still runs)"
    fi
    echo

    test_static_no_bashisms
    test_static_scanner_has_teeth
    test_static_scanner_ignores_comments
    test_zsh_parity
    test_zsh_caller_survives_the_call
    test_zsh_end_to_end_resolution
    test_zsh_shared_branch_classifier_parity

    echo
    echo "pass=$pass fail=$fail skip=$skip"
    [ "$fail" -eq 0 ]
}

run_all
