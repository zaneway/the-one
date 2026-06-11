#!/usr/bin/env python3
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("theone-hook-session.py")


class HookSessionTest(unittest.TestCase):
    def test_end_outputs_no_ingest(self):
        with tempfile.TemporaryDirectory() as tmp:
            binding = Path(tmp) / "binding.cursor.json"
            binding.write_text(
                json.dumps({"session_id": "sess_test", "task_id": "task_test"}),
                encoding="utf-8",
            )

            proc = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "end",
                    "--agent",
                    "cursor",
                    "--binding",
                    str(binding),
                ],
                input=json.dumps({"conversation_id": "sess_test"}),
                text=True,
                capture_output=True,
                check=True,
            )

            self.assertEqual(proc.stdout, "")


if __name__ == "__main__":
    unittest.main()
