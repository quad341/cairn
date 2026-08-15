#!/usr/bin/env python3

import importlib.util
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).parent
SPEC = importlib.util.spec_from_file_location("decision_probe", HERE / "decision_probe.py")
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class DecisionProbeTest(unittest.TestCase):
    def test_reconstructs_codex_prefix_and_cuts_post_situation_plan(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "codex.jsonl"
            rows = [
                {"type": "response_item", "payload": {"type": "message", "role": "user",
                 "content": [{"type": "input_text", "text": "Synthetic fixture: diagnose the frobnicator."}]}},
                {"type": "response_item", "payload": {"type": "message", "role": "assistant",
                 "content": [{"type": "output_text", "text":
                              "The synthetic frobnicator vanished after restart. I will inspect logs now."}]}},
            ]
            path.write_text("".join(json.dumps(row) + "\n" for row in rows))
            prefix, line, dropped = probe.reconstruct_prefix(
                path, "The synthetic frobnicator vanished after restart.", 10000, 5000
            )
            self.assertEqual(line, 2)
            self.assertEqual(dropped, 0)
            self.assertIn("Synthetic fixture", prefix)
            self.assertIn("The synthetic frobnicator vanished after restart.", prefix)
            self.assertNotIn("inspect logs", prefix)

    def test_reconstructs_claude_messages_but_excludes_hook_attachments(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "claude.jsonl"
            rows = [
                {"type": "attachment", "attachment": {"stdout": "cairn search leaked"}},
                {"type": "user", "message": {"role": "user", "content": "Fix it"}},
                {"type": "assistant", "message": {"role": "assistant", "content": [
                    {"type": "text", "text": "The queue is unexpectedly empty. Next I query it."}
                ]}},
            ]
            path.write_text("".join(json.dumps(row) + "\n" for row in rows))
            prefix, _, _ = probe.reconstruct_prefix(
                path, "The queue is unexpectedly empty.", 10000, 5000
            )
            self.assertIn("Fix it", prefix)
            self.assertNotIn("cairn search leaked", prefix)
            self.assertNotIn("Next I query", prefix)

    def test_decision_score_requires_explicit_cairn(self):
        self.assertEqual(probe.score_decision("- Search Cairn for the symptom."),
                         (True, "- Search Cairn for the symptom."))
        self.assertTrue(probe.score_decision("- Run `cairn get abc`.")[0])
        self.assertTrue(probe.score_decision("- Investigate the warning via cairn.")[0])
        self.assertEqual(probe.score_decision("- Search the logs and documentation."),
                         (False, None))

    def test_usefulness_parser_is_strict_and_accepts_fence(self):
        label, evidence = probe.parse_usefulness(
            '```json\n{"classification":"actively-misleads","evidence":"wrong branch"}\n```'
        )
        self.assertEqual(label, "actively-misleads")
        self.assertEqual(evidence, "wrong branch")
        self.assertEqual(probe.parse_usefulness('{"classification":"helpful"}')[0],
                         "unscored")

    def test_decision_prompts_differ_only_by_cairn_block(self):
        prefix = "<user>\nSomething broke\n</user>"
        absent = probe.decision_prompt(prefix)
        present = probe.decision_prompt(prefix, '{"triggers":[]}')
        self.assertNotIn("Cairn", absent)
        self.assertIn(prefix, present)
        self.assertIn(probe.DECISION_INSTRUCTION, present)
        self.assertIn("<cairn-prime>", present)

    def test_cli_writes_three_auditable_records_without_touching_real_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            store, output = root / "store-copy", root / "out"
            store.mkdir()
            transcript = root / "session.jsonl"
            transcript.write_text(json.dumps({
                "type": "assistant", "message": {"role": "assistant", "content": [
                    {"type": "text", "text": "The synthetic widget vanished. I will inspect logs."}
                ]}
            }) + "\n")
            eval_path = root / "eval.jsonl"
            eval_path.write_text(json.dumps({
                "id": "synthetic-pair-1", "situation": "The synthetic widget vanished.",
                "source": str(transcript), "source_excerpt": "The synthetic widget vanished."
            }) + "\n")

            cairn = root / "fake-cairn"
            cairn.write_text("""#!/bin/sh
case "$1" in
prime) printf '{"triggers":[]}\\n' ;;
search) printf '{"hits":[{"id":"synthetic-entry-1"}]}\\n' ;;
get) printf '%s\\n' '+++ entry +++' ;;
*) exit 2 ;;
esac
""")
            model = root / "fake-model"
            model.write_text("""#!/bin/sh
prompt=$(cat)
case "$prompt" in
*cairn-entry*) result='{"classification":"no-change","evidence":"already known"}' ;;
*cairn-prime*) result='- Run cairn search for the symptom.' ;;
*) result='- Inspect logs.' ;;
esac
python3 -c 'import json,sys; print(json.dumps({"result":sys.argv[1]}))' "$result"
""")
            os.chmod(cairn, 0o755)
            os.chmod(model, 0o755)
            subprocess.run([
                str(HERE / "decision_probe.py"), "--eval", str(eval_path),
                "--store", str(store), "--cairn-bin", str(cairn),
                "--output-dir", str(output), "--model-command", str(model),
            ], check=True, capture_output=True, text=True)

            records = [json.loads(line) for line in
                       (output / "results.jsonl").read_text().splitlines()]
            self.assertEqual(len(records), 3)
            absent = next(r for r in records if r.get("arm") == "cairn-absent")
            present = next(r for r in records if r.get("arm") == "cairn-present")
            useful = next(r for r in records if r["mode"] == "usefulness")
            self.assertFalse(absent["consults_cairn"])
            self.assertTrue(present["consults_cairn"])
            self.assertEqual(useful["classification"], "no-change")
            self.assertIn("<conversation-prefix>", absent["prompt"])
            self.assertTrue((output / "audit.md").is_file())


if __name__ == "__main__":
    unittest.main()
