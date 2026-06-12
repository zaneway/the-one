#!/usr/bin/env python3
import json
import os
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

    def test_start_derives_project_and_repo_from_hook_cwd(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            project_root = tmp_path / "workspace-root"
            conversation_dir = project_root / "services" / "order-service"
            conversation_dir.mkdir(parents=True)
            env = dict(os.environ)
            env.pop("THEONE_PROJECT_ID", None)
            env.pop("THEONE_REPO_ID", None)
            env["THEONE_PROJECT_DIR"] = str(project_root)
            env.pop("ROOT_DIR", None)

            proc = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "start",
                    "--agent",
                    "claude_code",
                ],
                input=json.dumps({"session_id": "sess_order", "cwd": str(conversation_dir)}),
                text=True,
                capture_output=True,
                check=True,
                cwd=project_root,
                env=env,
            )

            envelope = json.loads(proc.stdout)
            payload = envelope["events"][0]["payload"]
            self.assertEqual(payload["project_id"], "order-service")
            self.assertEqual(payload["repo_id"], "order-service")


if __name__ == "__main__":
    unittest.main()
