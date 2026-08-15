#!/usr/bin/env python3
"""Cheap, auditable probes for Cairn consultation decisions and hit usefulness."""

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
from pathlib import Path

DEFAULT_FORBIDDEN_STORE = Path.home() / "projects" / "cairn-store"
DECISION_INSTRUCTION = (
    "Based only on the conversation so far, state what you would do next. "
    "Be concrete, but do not execute tools. Return a short plan of 2-5 bullets."
)
CAIRN_AVAILABLE = """Cairn is available in this session as a read-only knowledge cache.
- `cairn search "<free text situation>"` finds ranked lexical candidates.
- `cairn list <topic_key>` resolves an exact known topic.
- `cairn get <id>` reads an entry before you act on it.
The SessionStart hook injected this payload:
<cairn-prime>
{prime}
</cairn-prime>"""


def text_parts(content):
    if isinstance(content, str):
        return [content]
    out = []
    for part in content or []:
        if not isinstance(part, dict):
            continue
        if isinstance(part.get("text"), str):
            out.append(part["text"])
        elif part.get("type") == "tool_result":
            value = part.get("content")
            if isinstance(value, str):
                out.append(value)
            elif isinstance(value, list):
                out.extend(text_parts(value))
    return out


def transcript_message(record):
    """Return (role, text) for Claude/Codex conversational records only."""
    kind = record.get("type")
    payload = record.get("payload") or {}
    if kind == "response_item":
        if payload.get("type") == "message" and payload.get("role") in ("user", "assistant"):
            return payload["role"], "\n".join(text_parts(payload.get("content")))
        if payload.get("type") == "function_call_output":
            return "tool", str(payload.get("output") or "")
    if kind in ("user", "assistant"):
        message = record.get("message") or {}
        role = message.get("role") or kind
        return role, "\n".join(text_parts(message.get("content")))
    return None


def comparable(text):
    return re.sub(r"[^a-z0-9]+", " ", text.lower()).strip()


def reconstruct_prefix(path, situation, max_chars, max_message_chars, source_excerpt=None):
    messages = []
    target = comparable(situation)
    target_head = " ".join(target.split()[:12])
    excerpt = comparable(source_excerpt or "")
    excerpt_head = " ".join(excerpt.split()[:12])
    found = False
    with open(path, errors="replace") as fh:
        for line_no, line in enumerate(fh, 1):
            try:
                item = transcript_message(json.loads(line))
            except (json.JSONDecodeError, TypeError):
                continue
            if not item or not item[1].strip():
                continue
            role, text = item
            normalized = comparable(text)
            if target and (target in normalized or (target_head and target_head in normalized) or
                           (excerpt and excerpt in normalized) or
                           (excerpt_head and excerpt_head in normalized)):
                # Stop at the pre-answer situation itself. The rest of the source
                # message often states the action the historical agent chose and
                # would leak the label into both arms.
                messages.append((role, situation.strip()))
                found = True
                matched_line = line_no
                break
            clipped = text.strip()
            if len(clipped) > max_message_chars:
                clipped = "[earlier content clipped]\n" + clipped[-max_message_chars:]
            messages.append((role, clipped))
    if not found:
        raise ValueError(f"situation not found in transcript: {path}")

    kept, used = [], 0
    for role, text in reversed(messages):
        rendered = f"<{role}>\n{text}\n</{role}>"
        if kept and used + len(rendered) > max_chars:
            break
        kept.append(rendered)
        used += len(rendered)
    kept.reverse()
    return "\n\n".join(kept), matched_line, len(messages) - len(kept)


def all_scope_tags(store):
    tags = set()
    for md in Path(store).rglob("*.md"):
        text = md.read_text(errors="replace")
        for match in re.finditer(r"scope\s*=\s*\[([^]]*)]", text):
            tags.update(re.findall(r'"([^"]+)"', match.group(1)))
    return sorted(tags)


def cairn_env(store, identity):
    env = os.environ.copy()
    env["CAIRN_STORE"] = str(store)
    env["CAIRN_IDENTITY"] = " ".join(identity)
    return env


def run_cairn(binary, store, identity, args):
    proc = subprocess.run(
        [binary, *args], text=True, capture_output=True,
        env=cairn_env(store, identity), check=False,
    )
    if proc.returncode:
        raise RuntimeError(f"cairn {' '.join(args)} failed: {proc.stderr.strip()}")
    return proc.stdout


def parse_search(stdout):
    value = json.loads(stdout)
    if isinstance(value, dict):
        return value.get("hits") or []
    raise ValueError("search output is not a JSON object")


def default_model_command(model):
    claude = shutil.which("claude")
    if not claude:
        raise RuntimeError("claude not found on PATH; pass --model-command")
    return [
        claude, "--print", "--no-session-persistence",
        "--output-format", "json", "--permission-mode", "dontAsk",
        "--disable-slash-commands", "--no-chrome", "--model", model,
        "--settings", '{"hooks":{"SessionStart":[]}}',
    ]


def run_model(command, prompt, timeout):
    proc = subprocess.run(
        command, input=prompt, text=True, capture_output=True,
        cwd="/tmp", timeout=timeout, check=False,
        env={**os.environ, "CLAUDECODE": ""},
    )
    if proc.returncode:
        raise RuntimeError(
            f"model command failed ({proc.returncode}): {proc.stderr.strip()}"
        )
    raw = proc.stdout
    response = raw
    try:
        envelope = json.loads(raw)
        if isinstance(envelope, dict) and isinstance(envelope.get("result"), str):
            response = envelope["result"]
    except json.JSONDecodeError:
        pass
    return response.strip(), raw.strip(), proc.stderr.strip()


CAIRN_COMMAND_RE = re.compile(r"(?i)`?cairn\s+(?:search|list|get|prime)\b")
CONSULT_VERB_RE = re.compile(
    r"(?i)\b(?:consult|check|query|search|read|investigate|look(?:\s+it)?\s+up)\b"
)


def score_decision(response):
    for line in response.splitlines():
        if CAIRN_COMMAND_RE.search(line) or (re.search(r"(?i)\bcairn\b", line) and
                                             CONSULT_VERB_RE.search(line)):
            return True, line.strip()
    return False, None


def parse_usefulness(response):
    candidate = response.strip()
    fenced = re.search(r"```(?:json)?\s*(\{.*?})\s*```", candidate, re.S)
    if fenced:
        candidate = fenced.group(1)
    else:
        start, end = candidate.find("{"), candidate.rfind("}")
        if start >= 0 and end > start:
            candidate = candidate[start:end + 1]
    try:
        value = json.loads(candidate)
    except json.JSONDecodeError:
        return "unscored", None
    label = value.get("classification")
    if label not in ("changes-plan", "no-change", "actively-misleads"):
        return "unscored", value.get("evidence")
    return label, value.get("evidence")


def decision_prompt(prefix, prime=None):
    blocks = ["<conversation-prefix>", prefix, "</conversation-prefix>"]
    if prime is not None:
        blocks.extend(["", CAIRN_AVAILABLE.format(prime=prime.rstrip())])
    blocks.extend(["", DECISION_INSTRUCTION])
    return "\n".join(blocks)


def usefulness_prompt(prefix, entry_id, entry):
    return f"""<conversation-prefix>
{prefix}
</conversation-prefix>

The following Cairn entry was the top result returned for this situation. Treat it as cached prior work, not guaranteed truth.
<cairn-entry id={json.dumps(entry_id)}>
{entry.rstrip()}
</cairn-entry>

Would this entry change the next plan implied by the conversation? Judge operational effect, not topical similarity. Use `actively-misleads` if following it would cause a materially wrong action; use `no-change` if it is irrelevant, redundant, or too vague; use `changes-plan` only if it supplies a useful new action, constraint, or diagnosis.

Return exactly one JSON object:
{{"classification":"changes-plan|no-change|actively-misleads","evidence":"one concise reason describing the plan effect"}}"""


def write_outputs(output_dir, records, metadata):
    output_dir.mkdir(parents=True, exist_ok=True)
    with open(output_dir / "results.jsonl", "w") as fh:
        for record in records:
            fh.write(json.dumps(record, ensure_ascii=False) + "\n")

    decision = [r for r in records if r["mode"] == "decision"]
    useful = [r for r in records if r["mode"] == "usefulness"]
    b = [r for r in decision if r["arm"] == "cairn-present"]
    a = [r for r in decision if r["arm"] == "cairn-absent"]
    lines = ["# Decision probe summary", "", f"Model command: `{' '.join(metadata['model_command'])}`", ""]
    lines += ["| Metric | Count |", "|---|---:|",
              f"| Absent-arm consultation | {sum(r['consults_cairn'] for r in a)}/{len(a)} |",
              f"| Present-arm consultation | {sum(r['consults_cairn'] for r in b)}/{len(b)} |",
              f"| Paired decision lift | {sum(r['consults_cairn'] for r in b) - sum(r['consults_cairn'] for r in a):+d} |"]
    if useful:
        for label in ("changes-plan", "no-change", "actively-misleads", "unscored"):
            lines.append(f"| Usefulness: {label} | {sum(r['classification'] == label for r in useful)}/{len(useful)} |")
    lines += ["", "## Per-pair", "", "| Pair | Absent | Present | Usefulness | Returned entry |", "|---|---:|---:|---|---|"]
    ids = list(dict.fromkeys(r["eval_id"] for r in records))
    for eid in ids:
        ar = next((r for r in a if r["eval_id"] == eid), None)
        br = next((r for r in b if r["eval_id"] == eid), None)
        ur = next((r for r in useful if r["eval_id"] == eid), None)
        lines.append(f"| {eid} | {str(ar and ar['consults_cairn']).lower()} | {str(br and br['consults_cairn']).lower()} | {ur['classification'] if ur else '—'} | {ur['returned_entry_id'] if ur else '—'} |")
    (output_dir / "summary.md").write_text("\n".join(lines) + "\n")

    audit = ["# Decision probe audit", "", "Every prompt and raw response is reproduced verbatim below."]
    for r in records:
        audit += ["", f"## {r['eval_id']} — {r['mode']} / {r.get('arm', 'returned-hit')}", "",
                  "### Prompt", "", "```text", r["prompt"], "```", "", "### Raw response", "", "```text", r["raw_response"], "```"]
    (output_dir / "audit.md").write_text("\n".join(audit) + "\n")


def main():
    p = argparse.ArgumentParser(description="Probe whether a model consults Cairn and whether a returned hit helps")
    p.add_argument("--eval", type=Path, help="eval JSONL containing source/situation")
    p.add_argument("--store", type=Path, help="disposable copy of the Cairn store")
    p.add_argument("--cairn-bin")
    p.add_argument("--output-dir", type=Path)
    p.add_argument("--rescore", type=Path,
                   help="existing results.jsonl to rescore without model calls")
    p.add_argument("--mode", choices=("decision", "usefulness", "both"), default="both")
    p.add_argument("--ids", nargs="*", help="only these eval IDs")
    p.add_argument("--limit", type=int, default=0, help="first N selected pairs (0 = all)")
    p.add_argument("--identity", nargs="*", help="identity tags; default is explicit all-scope diagnostic identity")
    p.add_argument("--model", default="haiku")
    p.add_argument("--model-command", help="shell-split command; prompt is supplied on stdin")
    p.add_argument("--timeout", type=int, default=180)
    p.add_argument("--max-prefix-chars", type=int, default=24000)
    p.add_argument("--max-message-chars", type=int, default=5000)
    args = p.parse_args()

    if args.rescore:
        records = [json.loads(line) for line in args.rescore.read_text().splitlines()
                   if line.strip()]
        if not records:
            p.error("rescore file is empty")
        for record in records:
            if record.get("mode") == "decision":
                score, evidence = score_decision(record.get("raw_response", ""))
                record["consults_cairn"], record["evidence"] = score, evidence
            elif record.get("mode") == "usefulness" and record.get("raw_response"):
                label, evidence = parse_usefulness(record["raw_response"])
                record["classification"], record["evidence"] = label, evidence
        output_dir = args.output_dir or args.rescore.parent
        write_outputs(output_dir, records, {"model_command": records[0]["model_command"]})
        print(output_dir / "results.jsonl")
        print(output_dir / "summary.md")
        print(output_dir / "audit.md")
        return 0

    for required in ("eval", "store", "cairn_bin", "output_dir"):
        if getattr(args, required) is None:
            p.error(f"--{required.replace('_', '-')} is required unless --rescore is used")

    if args.store.resolve() == DEFAULT_FORBIDDEN_STORE.resolve():
        p.error(f"refusing original store; copy {DEFAULT_FORBIDDEN_STORE} to /tmp first")
    if not args.store.is_dir():
        p.error(f"store does not exist: {args.store}")
    command = shlex.split(args.model_command) if args.model_command else default_model_command(args.model)
    fallback_identity = all_scope_tags(args.store)
    rows = [json.loads(line) for line in args.eval.read_text().splitlines() if line.strip()]
    if args.ids:
        wanted = set(args.ids)
        rows = [row for row in rows if row.get("id") in wanted]
    if args.limit:
        rows = rows[:args.limit]
    if not rows:
        p.error("no eval pairs selected")

    primes = {}
    records = []
    run_id = dt.datetime.now(dt.timezone.utc).isoformat()
    for row in rows:
        if args.identity is not None:
            identity, identity_source = args.identity, "command line"
        elif row.get("identity_tags") is not None or row.get("identity") is not None:
            identity = row.get("identity_tags", row.get("identity"))
            if isinstance(identity, str):
                identity = identity.split()
            identity_source = "eval pair"
        else:
            identity = fallback_identity
            identity_source = "all-store-scope diagnostic superset"
        identity = sorted(identity)
        identity_key = tuple(identity)
        if identity_key not in primes:
            primes[identity_key] = run_cairn(
                args.cairn_bin, args.store, identity, ["prime", "--json"]
            )
        prime = primes[identity_key]
        prefix, matched_line, dropped = reconstruct_prefix(
            row["source"], row["situation"], args.max_prefix_chars,
            args.max_message_chars, row.get("source_excerpt")
        )
        common = {
            "eval_id": row["id"], "source": row["source"],
            "source_line": matched_line, "prefix_messages_dropped": dropped,
            "identity": identity, "identity_source": identity_source,
            "model_command": command, "run_id": run_id,
        }
        if args.mode in ("decision", "both"):
            arms = [("cairn-absent", None), ("cairn-present", prime)]
            # Stable alternation avoids making one treatment systematically
            # first while keeping reruns directly comparable.
            if hashlib.sha256(row["id"].encode()).digest()[0] & 1:
                arms.reverse()
            for arm_order, (arm, payload) in enumerate(arms, 1):
                prompt = decision_prompt(prefix, payload)
                response, raw, stderr = run_model(command, prompt, args.timeout)
                consults, evidence = score_decision(response)
                records.append({**common, "mode": "decision", "arm": arm,
                                "arm_order": arm_order,
                                "consults_cairn": consults, "evidence": evidence,
                                "prompt": prompt, "raw_response": response,
                                "raw_model_output": raw, "model_stderr": stderr})
        if args.mode in ("usefulness", "both"):
            search = run_cairn(args.cairn_bin, args.store, identity,
                               ["search", row["situation"], "--json", "--limit", "1"])
            hits = parse_search(search)
            if not hits:
                records.append({**common, "mode": "usefulness", "classification": "no-hit",
                                "returned_entry_id": None, "evidence": None,
                                "prompt": "", "raw_response": "", "raw_model_output": "",
                                "model_stderr": "", "search_output": search})
            else:
                entry_id = hits[0]["id"]
                entry = run_cairn(args.cairn_bin, args.store, identity, ["get", entry_id])
                prompt = usefulness_prompt(prefix, entry_id, entry)
                response, raw, stderr = run_model(command, prompt, args.timeout)
                classification, evidence = parse_usefulness(response)
                records.append({**common, "mode": "usefulness",
                                "classification": classification,
                                "returned_entry_id": entry_id, "evidence": evidence,
                                "prompt": prompt, "raw_response": response,
                                "raw_model_output": raw, "model_stderr": stderr,
                                "search_output": search})
        write_outputs(args.output_dir, records, {"model_command": command})
    print(args.output_dir / "results.jsonl")
    print(args.output_dir / "summary.md")
    print(args.output_dir / "audit.md")
    return 0


if __name__ == "__main__":
    sys.exit(main())
