#!/usr/bin/env python3
"""P5：session.start / session.end 包络与 runtime 清理（禁止在 shell 内嵌 Python）。"""

from __future__ import annotations

import argparse
import json
import os
import sys
import uuid

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
runtime_cache_name = _rt.runtime_cache_name
format_structured_content_summary = _rt.format_structured_content_summary


def _read_stdin_json() -> dict:
    raw = sys.stdin.read()
    if not raw.strip():
        return {}
    try:
        data = json.loads(raw)
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}


def _load_binding(path: str) -> dict:
    if not path or not os.path.isfile(path):
        return {}
    try:
        with open(path, "r", encoding="utf-8") as handle:
            data = json.load(handle)
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}


def cmd_start(args: argparse.Namespace) -> int:
    data = _read_stdin_json()
    agent = (args.agent or "cursor").strip() or "cursor"
    session_id = pick(
        data,
        ["session_id", "sessionId", "conversation_id", "conversationId"],
        "",
    )
    if not session_id:
        return 0

    if agent == "claude_code":
        source = pick(data, ["source"], "startup")
        goal = f"Claude Code session ({source})"
        default_task = f"task_{agent}_auto"
        content = "Claude Code session start"
        producer = f"{agent}_hook:sessionStart"
    elif agent == "codex":
        source = pick(data, ["source"], "startup")
        goal = f"Codex session ({source})"
        default_task = "task_codex_auto"
        content = "Codex session start"
        producer = "codex_hook:SessionStart"
    else:
        goal = pick(data, ["goal", "goal_summary", "summary"], "Cursor session start")
        default_task = "task_cursor_auto"
        content = "Cursor session start"
        producer = "cursor_hook:sessionStart"

    task_id = pick(data, ["task_id", "taskId"], default_task)
    content_summary, _ = format_structured_content_summary(
        "session.start",
        f"会话生命周期：{content}",
        status_text="active",
    )
    envelope = {
        "ingest_id": "ing_" + uuid.uuid4().hex[:16],
        "protocol_version": "v1",
        "producer": producer,
        "agent_type": agent,
        "session_id": session_id,
        "events": [
            {
                "kind": "session.lifecycle",
                "event_type": "session.start",
                "payload": {
                    "agent_type": agent,
                    "workspace_id": "local_default_workspace",
                    "project_id": "the-one",
                    "repo_id": "the-one",
                    "conversation_id": session_id,
                    "content_summary": content_summary,
                    "capture_capabilities": {
                        "conversation_capture": True,
                        "tool_call_capture": True,
                        "tool_output_capture": True,
                        "file_edit_capture": True,
                        "session_lifecycle": True,
                        "mcp_observe": True,
                    },
                    "session": {"goal_summary": goal, "status": "active"},
                    "task": {
                        "task_summary": task_id,
                        "status": "active",
                        "outcome_summary": "",
                    },
                },
            }
        ],
    }
    print(json.dumps(envelope, ensure_ascii=False))
    return 0


def cmd_end(args: argparse.Namespace) -> int:
    return 0


def cmd_cleanup(args: argparse.Namespace) -> int:
    agent = (args.agent or "cursor").strip() or "cursor"
    state_dir = args.state_dir
    binding_file = args.binding
    surface_file = args.surface

    names = [
        "prompt-cache.json",
        "inject-cache.json",
        "prefetch.json",
        "context-cache.json",
    ]
    paths = [binding_file, os.path.join(state_dir, "turn-dedup.json"), os.path.join(state_dir, "atomic-dedup.json")]
    for name in names:
        paths.append(os.path.join(state_dir, runtime_cache_name(name, agent)))
    paths.extend(
        [
            os.path.join(state_dir, "context-cache.error.log"),
            os.path.join(state_dir, "session.json"),
            os.path.join(state_dir, "session.json.bak"),
        ]
    )
    for path in paths:
        if path and os.path.isfile(path):
            try:
                os.remove(path)
            except Exception:
                pass

    if not surface_file:
        return 0
    os.makedirs(os.path.dirname(surface_file), exist_ok=True)
    if agent in {"claude_code", "codex"}:
        body = (
            f"# The One 记忆上下文（{agent} 自动注入）\n\n"
            "_（暂无命中记忆；新会话或 prefetch 后将自动更新。）_\n"
        )
    else:
        body = (
            "---\n"
            "description: The One 本轮记忆上下文（beforeSubmitPrompt 自动刷新，勿手工编辑）\n"
            "alwaysApply: true\n"
            "---\n\n"
            "_（暂无命中记忆；新会话或 prefetch 后将自动更新。）_\n"
        )
    with open(surface_file, "w", encoding="utf-8") as handle:
        handle.write(body)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="theone session hook helpers")
    sub = parser.add_subparsers(dest="command", required=True)

    start = sub.add_parser("start")
    start.add_argument("--agent", required=True)
    start.set_defaults(func=cmd_start)

    end = sub.add_parser("end")
    end.add_argument("--agent", required=True)
    end.add_argument("--binding", required=True)
    end.set_defaults(func=cmd_end)

    clean = sub.add_parser("cleanup-runtime")
    clean.add_argument("--agent", required=True)
    clean.add_argument("--state-dir", required=True)
    clean.add_argument("--binding", required=True)
    clean.add_argument("--surface", required=True)
    clean.set_defaults(func=cmd_cleanup)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
