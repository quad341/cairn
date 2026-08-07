#!/usr/bin/env python3
"""Recall hit-rate: how much of what `cairn prime` surfaced actually got used.

WHAT THIS MEASURES
Store size and hit-rate are different questions (crn-894i). This joins each
`cairn get` call (obslog retrieval_outcome, outcome hit/stale) against the
nearest-preceding `cairn prime` call for the same identity (obslog
prime_emit), and classifies whether the entry that was successfully
retrieved had actually been surfaced by that prime call. An entry can be
"in the store" (hit) yet never surfaced by prime -- recall-miss is exactly
that failure mode.

JOIN ALGORITHM
For each retrieval_outcome with outcome in {hit, stale} (a miss means the
entry was not in the store at all -- a different failure mode, counted
separately as miss_excluded and left out of hit-rate/join-coverage):
  - find the nearest-preceding prime_emit record with the same
    identity_tags (by ts; "nearest" matters -- an older prime_emit for the
    same identity may have surfaced a different item set than a more
    recent one)
  - no such record -> unjoined
  - found, entry_id in that record's item_ids -> surfaced
  - found, entry_id not in item_ids -> recall_miss
"""
import argparse, collections, datetime, glob, json, os, sys

C = {"hdr": "\033[1;36m", "b": "\033[1m", "dim": "\033[2m", "warn": "\033[33m",
     "hot": "\033[31m", "ok": "\033[32m", "off": "\033[0m"}


def dedupe(paths):
    """Drop repeats, comparing by realpath so a symlink cannot double-count."""
    seen, out = set(), []
    for p in paths:
        p = os.path.expanduser(p.strip())
        if not p:
            continue
        real = os.path.realpath(p)
        if real not in seen:
            seen.add(real)
            out.append(p)
    return out


def discover_homes():
    """Find account HOMEs by the fleet's own *-claude sibling convention
    (same convention burn-report uses for its own discovery).
    """
    home = os.path.expanduser("~")
    roots = {home, os.path.dirname(home)}
    cands = set(roots)
    for r in roots:
        cands.update(glob.glob(os.path.join(r, "*-claude")))
    return dedupe(sorted(c for c in cands
                         if os.path.isdir(os.path.join(c, ".local", "state", "cairn"))))


def resolve_homes(cli):
    """--homes beats $RECALL_HITRATE_HOMES beats discovery."""
    if cli:
        return dedupe(cli), "--homes"
    env = os.environ.get("RECALL_HITRATE_HOMES", "")
    if env.strip():
        return dedupe(env.replace(",", ":").split(":")), "$RECALL_HITRATE_HOMES"
    return discover_homes(), "auto-discovered"


def log_path_for_home(home):
    """Mirror internal/obslog/path.go's LogPath(): $XDG_STATE_HOME/cairn/debug.jsonl,
    falling back to $HOME/.local/state/cairn/debug.jsonl. XDG_STATE_HOME is a
    single env var for this process, so it can only override the path for
    the home that matches this process's own $HOME -- other scanned homes
    (siblings, on a multi-account box) always use the fallback, since their
    own override (if any) is not observable from here.
    """
    xdg = os.environ.get("XDG_STATE_HOME")
    if xdg and os.path.realpath(home) == os.path.realpath(os.path.expanduser("~")):
        return os.path.join(xdg, "cairn", "debug.jsonl")
    return os.path.join(home, ".local", "state", "cairn", "debug.jsonl")


def harvest(homes):
    """Parse each home's debug.jsonl and join retrieval_outcome records
    against the nearest-preceding prime_emit for the same identity_tags.

    Returns (counts, per_home): counts has surfaced/recall_miss/unjoined/
    miss_excluded; per_home is {home: n_records} for coverage reporting.
    """
    counts = collections.Counter()
    per_home = collections.Counter()
    for home in homes:
        path = log_path_for_home(home)
        # Time-ordered per identity as records are read; file order is
        # assumed chronological (obslog only ever appends), so a prime_emit
        # can only join retrieval_outcomes that come after it in the file.
        primes = collections.defaultdict(list)
        try:
            fh = open(path, errors="ignore")
        except OSError:
            continue
        with fh:
            for line in fh:
                try:
                    d = json.loads(line)
                except Exception:
                    continue
                kind = d.get("kind")
                if kind not in ("prime_emit", "retrieval_outcome"):
                    continue
                try:
                    when = datetime.datetime.fromisoformat(
                        (d.get("ts") or "").replace("Z", "+00:00"))
                except ValueError:
                    continue
                identity = tuple(sorted(d.get("identity_tags") or []))
                per_home[home] += 1
                if kind == "prime_emit":
                    primes[identity].append((when, d.get("item_ids") or []))
                    continue
                outcome = d.get("outcome")
                if outcome == "miss":
                    counts["miss_excluded"] += 1
                    continue
                if outcome not in ("hit", "stale"):
                    continue
                nearest = None
                for pwhen, item_ids in primes.get(identity, ()):
                    if pwhen <= when and (nearest is None or pwhen > nearest[0]):
                        nearest = (pwhen, item_ids)
                if nearest is None:
                    counts["unjoined"] += 1
                elif d.get("entry_id") in nearest[1]:
                    counts["surfaced"] += 1
                else:
                    counts["recall_miss"] += 1
    return counts, per_home


def main():
    p = argparse.ArgumentParser(
        description="Recall hit-rate: join cairn prime_emit against retrieval_outcome")
    p.add_argument("--json", action="store_true", help="machine-readable output")
    p.add_argument("--homes", nargs="*", default=None,
                   help="account HOMEs to scan; overrides $RECALL_HITRATE_HOMES "
                        "(':' or ',' separated), which overrides auto-discovery "
                        "of *-claude dirs under $HOME and its parent")
    a = p.parse_args()

    homes, provenance = resolve_homes(a.homes)
    if not homes:
        print("No cairn account HOMEs found. Auto-discovery looks for a "
              ".local/state/cairn dir under $HOME, its parent, and their "
              "*-claude siblings; pass --homes or set $RECALL_HITRATE_HOMES.",
              file=sys.stderr)
        return 1

    counts, per_home = harvest(homes)
    scanned = sum(per_home.values())

    src = ", ".join(f"{os.path.basename(h.rstrip('/')) or h} {per_home.get(h, 0)}"
                    for h in homes)
    empty = [h for h in homes if not per_home.get(h)]
    if empty and provenance != "auto-discovered":
        print(f"warning: no prime_emit/retrieval_outcome records found under "
              f"{', '.join(empty)} — totals below omit whatever they hold",
              file=sys.stderr)

    surfaced = counts["surfaced"]
    recall_miss = counts["recall_miss"]
    unjoined = counts["unjoined"]
    miss_excluded = counts["miss_excluded"]
    total = surfaced + recall_miss + unjoined + miss_excluded

    if total == 0:
        print(f"No prime_emit/retrieval_outcome records found across "
              f"{len(homes)} home(s) ({provenance}). ", file=sys.stderr)
        return 1

    joined = surfaced + recall_miss
    hit_rate = round(surfaced / joined, 4) if joined else None
    coverage_denom = joined + unjoined
    join_coverage = round(joined / coverage_denom, 4) if coverage_denom else None

    if a.json:
        # stdout stays pure JSON, matching burn-report's contract for
        # machine consumers; coverage goes to stderr.
        print(f"sources ({provenance}): {src}", file=sys.stderr)
        print(json.dumps({
            "surfaced": surfaced,
            "recall_miss": recall_miss,
            "unjoined": unjoined,
            "miss_excluded": miss_excluded,
            "hit_rate": hit_rate,
            "join_coverage": join_coverage,
        }, indent=1))
        return 0

    def pct(x):
        return "n/a" if x is None else f"{x * 100:.1f}%"

    hr = hit_rate or 0
    hr_col = C["ok"] if hr >= 0.8 else (C["warn"] if hr >= 0.5 else C["hot"])
    print(f"{C['hdr']}{'=' * 78}{C['off']}")
    print(f"  {C['b']}Recall hit-rate{C['off']} {C['dim']}— {scanned} records "
          f"(crn-894i){C['off']}")
    print(f"  {C['dim']}sources ({provenance}): {src}{C['off']}")
    print(f"{C['hdr']}{'=' * 78}{C['off']}")
    print(f"  recall hit-rate:  {hr_col}{pct(hit_rate)}{C['off']}  "
          f"(surfaced {surfaced} / surfaced {surfaced} + recall_miss {recall_miss})")
    print(f"  join coverage:    {pct(join_coverage)}  "
          f"(joined {joined} / joined {joined} + unjoined {unjoined})")
    print(f"{C['hdr']}{'-' * 78}{C['off']}")
    print(f"  surfaced={surfaced}  recall_miss={recall_miss}  "
          f"unjoined={unjoined}  miss_excluded={miss_excluded}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
