#!/usr/bin/env python3
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Optional


SCRIPT = Path(__file__).with_name("theone-build-ingest.py")


class BuildIngestProducerTest(unittest.TestCase):
    def run_builder_stdout(self, mode: str, payload: dict, agent_type: str, prompt_cache_data: Optional[dict] = None) -> str:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            prompt_cache = tmp_path / "prompt-cache.json"
            binding = tmp_path / f"binding.{agent_type}.json"
            prompt_cache.write_text(
                json.dumps(prompt_cache_data or {"session_id": "sess_test", "task_id": "task_test"}),
                encoding="utf-8",
            )
            binding.write_text(
                json.dumps({"session_id": "sess_test", "task_id": "task_test"}),
                encoding="utf-8",
            )
            env = dict(os.environ)
            env["THEONE_AGENT_TYPE"] = agent_type
            proc = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "--mode",
                    mode,
                    "--prompt-cache",
                    str(prompt_cache),
                    "--session-state",
                    str(binding),
                ],
                input=json.dumps(payload),
                text=True,
                capture_output=True,
                env=env,
                check=True,
            )
            return proc.stdout

    def run_builder(self, mode: str, payload: dict, agent_type: str, prompt_cache_data: Optional[dict] = None) -> dict:
        return json.loads(self.run_builder_stdout(mode, payload, agent_type, prompt_cache_data))

    def test_claude_non_file_post_tool_records_raw_payload(self):
        envelope = self.run_builder(
            "claude-post-tool",
            {
                "session_id": "sess_test",
                "hook_event_name": "PostToolUse",
                "tool_name": "Bash",
                "tool_input": {"command": "go test ./..."},
                "tool_response": {"stdout": "ok", "success": True},
            },
            "claude_code",
        )

        payload = envelope["events"][0]["payload"]
        self.assertEqual(envelope["events"][0]["event_type"], "tool.result.summary")
        self.assertEqual(payload["tool_name"], "Bash")
        self.assertEqual(payload["payload_schema"], "tool_result.v1")
        self.assertEqual(payload["redaction_state"], "raw")
        raw_payload = json.loads(payload["raw_payload_json"])
        self.assertEqual(raw_payload["tool_input"]["command"], "go test ./...")
        self.assertEqual(raw_payload["tool_response"]["stdout"], "ok")
        self.assertFalse(payload["truncation"]["truncated"])

    def test_cursor_atomic_tool_outputs_no_ingest(self):
        stdout = self.run_builder_stdout(
            "atomic-tool",
            {
                "session_id": "sess_test",
                "tool_name": "memory_search",
                "output_summary": "ok",
            },
            "cursor",
        )

        self.assertEqual(stdout, "")

    def test_atomic_file_records_change_metadata(self):
        envelope = self.run_builder(
            "atomic-file",
            {
                "session_id": "sess_test",
                "file_path": "internal/auth/middleware.go",
                "change_type": "modify",
                "symbol": "ValidateToken",
                "before_hash": "sha256:before",
                "after_hash": "sha256:after",
                "summary": "调整 token 过期判断边界",
            },
            "cursor",
        )

        payload = envelope["events"][0]["payload"]
        self.assertEqual(envelope["producer"], "cursor_hook:afterFileEdit")
        self.assertEqual(envelope["events"][0]["event_type"], "file.edit.summary")
        self.assertEqual(payload["file_path"], "internal/auth/middleware.go")
        self.assertEqual(payload["change_type"], "modify")
        self.assertEqual(payload["symbol"], "ValidateToken")
        self.assertEqual(payload["before_hash"], "sha256:before")
        self.assertEqual(payload["after_hash"], "sha256:after")
        self.assertIn("keywords", payload)
        self.assertIn("salient_spans", payload)
        self.assertEqual(payload["payload_schema"], "file_edit.v1")
        self.assertEqual(payload["redaction_state"], "raw")
        self.assertIn("raw_payload_json", payload)
        raw_payload = json.loads(payload["raw_payload_json"])
        self.assertEqual(raw_payload["file_path"], "internal/auth/middleware.go")
        self.assertEqual(raw_payload["summary"], "调整 token 过期判断边界")
        self.assertFalse(payload["truncation"]["truncated"])
        self.assertTrue(payload["raw_payload_hash"].startswith("sha256:"))

    def test_turn_agent_records_semantic_digest_metadata(self):
        envelope = self.run_builder(
            "turn-agent",
            {
                "conversation_id": "sess_test",
                "prompt": "请分析当前 memory capture 的边界条件",
                "response": "结论：保留结构化摘要、关键片段和原文哈希，不保存完整模型应答。",
            },
            "cursor",
        )

        payload = envelope["events"][0]["payload"]
        self.assertEqual(envelope["events"][0]["kind"], "turn.completed")
        self.assertEqual(envelope["events"][0]["event_type"], "turn.completed")
        self.assertEqual(payload["semantic_summary_version"], "semantic_digest_v1")
        self.assertEqual(payload["user_prompt_chars"], len("请分析当前 memory capture 的边界条件"))
        self.assertEqual(payload["agent_response_chars"], len("结论：保留结构化摘要、关键片段和原文哈希，不保存完整模型应答。"))
        self.assertIn("user_raw_payload", payload)
        self.assertIn("agent_raw_payload", payload)
        self.assertEqual(json.loads(payload["user_raw_payload"])["prompt"], "请分析当前 memory capture 的边界条件")
        self.assertEqual(json.loads(payload["agent_raw_payload"])["response"], "结论：保留结构化摘要、关键片段和原文哈希，不保存完整模型应答。")
        self.assertTrue(payload["user_raw_payload_hash"].startswith("sha256:"))
        self.assertTrue(payload["agent_raw_payload_hash"].startswith("sha256:"))
        self.assertFalse(payload["user_truncation"]["truncated"])
        self.assertFalse(payload["agent_truncation"]["truncated"])
        self.assertEqual(payload["payload_schema"], "turn.completed.v1")
        self.assertEqual(payload["redaction_state"], "raw")
        self.assertNotIn("response", payload)
        self.assertNotIn("prompt", payload)

    def test_turn_agent_uses_cached_original_prompt_length_when_prompt_missing(self):
        envelope = self.run_builder(
            "turn-agent",
            {
                "conversation_id": "sess_test",
                "response": "已完成语义摘要记录。",
            },
            "cursor",
            {
                "session_id": "sess_test",
                "task_id": "task_test",
                "user_summary": "截断后的用户摘要",
                "user_prompt_chars": 4096,
            },
        )

        payload = envelope["events"][0]["payload"]
        self.assertEqual(payload["user_prompt_chars"], 4096)
        self.assertNotIn("user_prompt_hash", payload)


if __name__ == "__main__":
    unittest.main()
