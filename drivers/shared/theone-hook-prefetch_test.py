#!/usr/bin/env python3
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("theone-hook-prefetch.py")


class HookPrefetchTest(unittest.TestCase):
    def test_prepare_uses_configured_prompt_cache_user_summary_limit(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            prompt_cache = tmp_path / "prompt-cache.json"
            config = tmp_path / "theone.yaml"
            config.write_text(
                "adapter:\n  prompt_cache_user_summary_max_chars: 12\n",
                encoding="utf-8",
            )

            proc = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "prepare",
                    "--agent",
                    "cursor",
                    "--prompt-cache",
                    str(prompt_cache),
                    "--surface",
                    str(tmp_path / "surface.mdc"),
                    "--config",
                    str(config),
                ],
                input=json.dumps({"prompt": "abcdefghijklmnopqrstuvwxyz", "conversation_id": "sess"}),
                text=True,
                capture_output=True,
                check=True,
            )

            prefetch = json.loads(proc.stdout)
            cached = json.loads(prompt_cache.read_text(encoding="utf-8"))
            self.assertEqual(cached["user_summary"], "abcdefghijkl")
            self.assertEqual(cached["user_prompt_chars"], len("abcdefghijklmnopqrstuvwxyz"))
            self.assertNotIn("user_prompt_hash", cached)
            self.assertNotIn("prompt_fingerprint", cached)
            self.assertTrue(cached["generation_id"].startswith("gen_"))
            self.assertEqual(prefetch["task"], "abcdefghijkl")

    def test_prepare_derives_project_and_repo_from_hook_cwd(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            project_root = tmp_path / "workspace-root"
            conversation_dir = project_root / "services" / "payment-gateway"
            conversation_dir.mkdir(parents=True)
            prompt_cache = tmp_path / "prompt-cache.json"
            env = dict(os.environ)
            env.pop("THEONE_PROJECT_ID", None)
            env.pop("THEONE_REPO_ID", None)
            env["THEONE_PROJECT_DIR"] = str(project_root)
            env.pop("ROOT_DIR", None)

            proc = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "prepare",
                    "--agent",
                    "claude_code",
                    "--prompt-cache",
                    str(prompt_cache),
                    "--surface",
                    str(tmp_path / "surface.md"),
                ],
                input=json.dumps({"prompt": "学习当前项目", "session_id": "sess", "cwd": str(conversation_dir)}),
                text=True,
                capture_output=True,
                check=True,
                cwd=project_root,
                env=env,
            )

            prefetch = json.loads(proc.stdout)
            self.assertEqual(prefetch["project_id"], "payment-gateway")
            self.assertEqual(prefetch["repo_id"], "payment-gateway")


if __name__ == "__main__":
    unittest.main()
