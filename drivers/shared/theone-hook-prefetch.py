#!/usr/bin/env python3
"""P5：prefetch 前准备与 Hook 响应格式化（禁止在 shell 内嵌 Python）。"""

from __future__ import annotations

import argparse
import json
import os
import sys
from datetime import datetime

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))


def _load_runtime():
    import importlib.util

    path = os.path.join(SCRIPT_DIR, "theone-runtime-lib.py")
    spec = importlib.util.spec_from_file_location("theone_runtime_lib", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


_rt = _load_runtime()
pick = _rt.pick

DEFAULT_PROMPT_CACHE_USER_SUMMARY_MAX_CHARS = 3000


def _read_stdin_json() -> dict:
    raw = sys.stdin.read()
    if not raw.strip():
        return {}
    try:
        data = json.loads(raw)
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}


def _prompt_cache_user_summary_max_chars(config_path: str) -> int:
    if not config_path:
        return DEFAULT_PROMPT_CACHE_USER_SUMMARY_MAX_CHARS
    try:
        with open(os.path.expanduser(config_path), "r", encoding="utf-8") as handle:
            section = ""
            for raw_line in handle:
                line = raw_line.split("#", 1)[0].rstrip()
                if not line.strip():
                    continue
                if not raw_line.startswith((" ", "\t")) and line.endswith(":"):
                    section = line[:-1].strip()
                    continue
                if section != "adapter":
                    continue
                stripped = line.strip()
                key = "prompt_cache_user_summary_max_chars:"
                if not stripped.startswith(key):
                    continue
                value = stripped[len(key):].strip().strip("\"'")
                parsed = int(value)
                if parsed > 0:
                    return parsed
    except Exception:
        pass
    return DEFAULT_PROMPT_CACHE_USER_SUMMARY_MAX_CHARS


def cmd_prepare(args: argparse.Namespace) -> int:
    data = _read_stdin_json()
    agent = (args.agent or "cursor").strip() or "cursor"

    if agent in {"claude_code", "codex"}:
        prompt = pick(data, ["prompt"], "")
        conversation_id = pick(
            data, ["session_id", "sessionId", "conversation_id", "conversationId"], ""
        )
    else:
        prompt = pick(data, ["prompt", "userMessage", "input", "message"], "")
        conversation_id = pick(
            data,
            ["conversation_id", "conversationId", "session_id", "sessionId"],
            "",
        )

    generation_id = pick(data, ["generation_id", "generationId"], "")
    user_summary_max_chars = _prompt_cache_user_summary_max_chars(args.config)
    user_summary = prompt[:user_summary_max_chars] if prompt else "用户输入摘要未直接可见"
    if not generation_id:
        generation_id = "gen_" + datetime.now().strftime("%Y%m%d%H%M%S%f")

    turn_id = ""
    if generation_id:
        turn_id = "turn_" + generation_id

    cache_payload = {
        "user_summary": user_summary,
        "user_prompt_chars": len(prompt or ""),
        "session_id": conversation_id,
        "conversation_id": conversation_id,
        "generation_id": generation_id,
        "turn_id": turn_id,
        "captured_at": datetime.now().astimezone().isoformat(),
    }
    os.makedirs(os.path.dirname(args.prompt_cache) or ".", exist_ok=True)
    with open(args.prompt_cache, "w", encoding="utf-8") as handle:
        json.dump(cache_payload, handle, ensure_ascii=False, indent=2)

    prefetch = {
        "task": user_summary,
        "workspace_id": "local_default_workspace",
        "project_id": "the-one",
        "repo_id": "the-one",
        "session_id": conversation_id,
        "conversation_id": conversation_id,
        "generation_id": generation_id,
        "agent_type": agent,
        "token_budget": 1200,
        "include_code_refs": True,
        "include_evidence_summary": True,
        "rule_file": args.surface,
    }
    print(json.dumps(prefetch, ensure_ascii=False))
    return 0


def cmd_format_response(args: argparse.Namespace) -> int:
    raw = sys.stdin.read().strip()
    if not raw:
        _print_hook_output(args.agent, {})
        return 0
    try:
        data = json.loads(raw)
    except Exception:
        _print_hook_output(args.agent, {})
        return 0
    _print_hook_output(args.agent, data if isinstance(data, dict) else {})
    return 0


def _print_hook_output(agent: str, data: dict) -> None:
    text = (data.get("inject_markdown") or "").strip()
    if agent in {"claude_code", "codex"}:
        if not text:
            print(
                json.dumps(
                    {
                        "continue": True,
                        "hookSpecificOutput": {
                            "hookEventName": "UserPromptSubmit",
                            "additionalContext": "",
                        },
                    },
                    ensure_ascii=False,
                )
            )
            return
        out = {
            "continue": True,
            "hookSpecificOutput": {
                "hookEventName": "UserPromptSubmit",
                "additionalContext": text,
            }
        }
        print(json.dumps(out, ensure_ascii=False))
        return
    out = {"continue": True}
    if text:
        out["additional_context"] = text
    print(json.dumps(out, ensure_ascii=False))


def main() -> int:
    parser = argparse.ArgumentParser(description="theone prefetch hook helpers")
    sub = parser.add_subparsers(dest="command", required=True)

    prep = sub.add_parser("prepare", help="stdin=hook JSON; write prompt-cache; stdout=prefetch req")
    prep.add_argument("--agent", required=True)
    prep.add_argument("--prompt-cache", required=True)
    prep.add_argument("--surface", required=True)
    prep.add_argument("--config", default="")
    prep.set_defaults(func=cmd_prepare)

    fmt = sub.add_parser("format-response", help="stdin=prefetch-context JSON; stdout=hook response")
    fmt.add_argument("--agent", required=True)
    fmt.set_defaults(func=cmd_format_response)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
