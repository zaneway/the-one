#!/usr/bin/env python3
"""Build BatchEnvelope JSON for `theone ingest` from Cursor / Claude Code hook stdin."""

from __future__ import annotations

import argparse
import json
import os
import sys
import uuid
from datetime import datetime

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
AGENT_TYPE = (os.environ.get("THEONE_AGENT_TYPE") or "cursor").strip() or "cursor"
CLAUDE_FILE_TOOLS = frozenset({"Write", "Edit", "MultiEdit", "NotebookEdit"})


def _load_runtime_lib():
    import importlib.util

    lib_path = os.path.join(SCRIPT_DIR, "theone-runtime-lib.py")
    spec = importlib.util.spec_from_file_location("theone_runtime_lib", lib_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load runtime lib from {lib_path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


_runtime = _load_runtime_lib()
pick = _runtime.pick
resolve_session_task = _runtime.resolve_session_task
build_turn_id = _runtime.build_turn_id
merge_inject_fields = _runtime.merge_inject_fields
load_inject_cache = _runtime.load_inject_cache


def _producer(suffix: str) -> str:
    return f"{AGENT_TYPE}_hook:{suffix}"


def _envelope_base(session_id: str, producer: str) -> dict:
    return {
        "ingest_id": "ing_" + uuid.uuid4().hex[:16],
        "protocol_version": "v1",
        "producer": producer,
        "agent_type": AGENT_TYPE,
        "session_id": session_id,
        "events": [],
    }


def _scope_payload(session_id: str, task_id: str, turn_id: str = "") -> dict:
    payload = {
        "agent_type": AGENT_TYPE,
        "workspace_id": "local_default_workspace",
        "project_id": "the-one",
        "repo_id": "the-one",
        "conversation_id": session_id,
        "task_id": task_id,
    }
    if turn_id:
        payload["turn_id"] = turn_id
    return payload


def _tool_input_dict(data: dict) -> dict:
    ti = data.get("tool_input")
    return ti if isinstance(ti, dict) else {}


def _tool_response_dict(data: dict) -> dict:
    tr = data.get("tool_response")
    return tr if isinstance(tr, dict) else {}


def normalize_claude_post_tool(data: dict) -> dict:
    tool = pick(data, ["tool_name"])
    ti = _tool_input_dict(data)
    tr = _tool_response_dict(data)
    session_id = pick(data, ["session_id", "sessionId"])
    event = pick(data, ["hook_event_name", "hookEventName"], "")
    is_failure = event == "PostToolUseFailure"
    out: dict = {"session_id": session_id}
    if tool in CLAUDE_FILE_TOOLS:
        fp = pick(ti, ["file_path", "filePath"], pick(tr, ["filePath", "file_path"], "unknown_file"))
        out.update(
            {
                "file_path": fp,
                "change_type": "modify",
                "summary": f"Claude {tool}: {fp}",
            }
        )
        return out
    cmd = pick(ti, ["command", "description", "url"], "")
    stdout = ""
    if isinstance(tr, dict):
        stdout = pick(tr, ["stdout", "content", "message"], "")
        if not stdout and tr.get("success") is False:
            stdout = pick(tr, ["stderr"], "tool failed")
    exit_code = 0
    if is_failure:
        exit_code = 1
        err_text = pick(
            data,
            ["error_message", "errorMessage", "error", "message"],
            "",
        )
        if err_text:
            stdout = err_text[:200]
        elif not stdout:
            stdout = "工具执行失败"
    if isinstance(tr, dict) and tr.get("interrupted"):
        exit_code = 1
    out.update(
        {
            "tool_name": tool or "unknown_tool",
            "input_summary": (cmd or json.dumps(ti, ensure_ascii=False)[:200])[:200],
            "output_summary": (stdout or ("工具执行失败" if is_failure else "工具执行完成"))[:200],
            "exit_code": exit_code,
        }
    )
    return out


def normalize_claude_stop(data: dict) -> dict:
    return {
        "session_id": pick(data, ["session_id", "sessionId"]),
        "response": pick(
            data,
            ["last_assistant_message", "lastAssistantMessage"],
            "Claude 已完成本轮响应",
        ),
    }


def build_atomic_file(
    hook_data: dict,
    *,
    prompt_cache_file: str,
    session_state_file: str,
) -> dict:
    session_id, task_id = resolve_session_task(
        hook_data,
        prompt_cache_file=prompt_cache_file,
        session_state_file=session_state_file,
        agent_type=AGENT_TYPE,
    )
    if not session_id:
        return {}
    file_path = pick(hook_data, ["file_path", "path", "relativePath", "target_file"], "unknown_file")
    change_type = pick(hook_data, ["change_type", "changeType"], "modify")
    summary = pick(hook_data, ["summary", "content_summary", "description"], "")
    if not summary:
        summary = f"文件修改：{file_path}"
    turn_id = build_turn_id(session_id, file_path, datetime.now().strftime("%Y%m%d%H%M%S"))
    env = _envelope_base(session_id, _producer("afterFileEdit"))
    payload = _scope_payload(session_id, task_id, turn_id)
    payload.update(
        {
            "content_summary": summary[:500],
            "file_path": file_path,
            "change_type": change_type,
            "keywords": [AGENT_TYPE, "hook", "file-edit", file_path],
            "salient_spans": [summary[:200]],
        }
    )
    env["events"] = [
        {
            "kind": "capture.atomic",
            "event_type": "file.edit.summary",
            "payload": payload,
        }
    ]
    return env


def build_atomic_tool(
    hook_data: dict,
    *,
    prompt_cache_file: str,
    session_state_file: str,
) -> dict:
    session_id, task_id = resolve_session_task(
        hook_data,
        prompt_cache_file=prompt_cache_file,
        session_state_file=session_state_file,
        agent_type=AGENT_TYPE,
    )
    if not session_id:
        return {}
    tool_name = pick(hook_data, ["tool_name", "toolName", "name", "mcp_server"], "unknown_tool")
    input_summary = pick(hook_data, ["input_summary", "input", "arguments"], "")[:200]
    output_summary = pick(hook_data, ["output_summary", "output", "result", "response"], "")[:200]
    exit_code = hook_data.get("exit_code", hook_data.get("exitCode", 0))
    try:
        exit_code = int(exit_code)
    except Exception:
        exit_code = 0
    if not output_summary:
        output_summary = "工具执行完成"
    turn_id = build_turn_id(session_id, tool_name, datetime.now().strftime("%Y%m%d%H%M%S"))
    env = _envelope_base(session_id, _producer("afterToolUse"))
    payload = _scope_payload(session_id, task_id, turn_id)
    payload.update(
        {
            "tool_name": tool_name,
            "input_summary": input_summary or "输入摘要不可用",
            "output_summary": output_summary,
            "exit_code": exit_code,
            "content_summary": f"工具结果：{tool_name}",
            "keywords": [AGENT_TYPE, "hook", "tool-result", tool_name],
            "salient_spans": [output_summary[:200]],
        }
    )
    env["events"] = [
        {
            "kind": "capture.atomic",
            "event_type": "tool.result.summary",
            "payload": payload,
        }
    ]
    return env


def build_turn_agent(
    hook_data: dict,
    *,
    prompt_cache_file: str,
    session_state_file: str,
    inject_cache_file: str,
) -> dict:
    session_id, task_id = resolve_session_task(
        hook_data,
        prompt_cache_file=prompt_cache_file,
        session_state_file=session_state_file,
        agent_type=AGENT_TYPE,
    )
    if not session_id:
        return {}
    response = pick(
        hook_data,
        ["response", "content", "assistantMessage", "output", "text", "last_assistant_message"],
        "Agent 已完成本轮响应",
    )
    user_summary = pick(
        hook_data,
        ["prompt", "userMessage", "input", "lastUserMessage"],
        "",
    )
    if not user_summary:
        try:
            with open(prompt_cache_file, "r", encoding="utf-8") as handle:
                cached = (json.load(handle).get("user_summary") or "").strip()
                if cached:
                    user_summary = cached
        except Exception:
            pass
    if not user_summary:
        user_summary = "用户输入摘要未直接可见"
    stamp = datetime.now().strftime("%Y%m%d%H%M%S")
    turn_id = build_turn_id(session_id, user_summary, response, stamp)
    env = _envelope_base(session_id, _producer("afterAgentResponse"))
    payload = _scope_payload(session_id, task_id, turn_id)
    payload.update(
        {
            "user_summary": user_summary[:1000],
            "agent_summary": response[:1800],
            "is_substantive": True,
            "started_at": datetime.now().astimezone().isoformat(),
            "completed_at": datetime.now().astimezone().isoformat(),
            "keywords": [AGENT_TYPE, "hook", "turn-completed"],
            "salient_spans": [response[:200]],
        }
    )
    prompt_cache = _runtime.load_json(prompt_cache_file) if prompt_cache_file else {}
    payload = merge_inject_fields(
        payload,
        load_inject_cache(inject_cache_file),
        prompt_cache=prompt_cache,
    )
    env["events"] = [
        {
            "kind": "turn.completed",
            "event_type": "agent.response.summary",
            "payload": payload,
        }
    ]
    return env


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--mode",
        choices=["atomic-file", "atomic-tool", "turn-agent", "claude-post-tool"],
        required=True,
    )
    parser.add_argument("--hook-stdin-file", default="")
    parser.add_argument("--prompt-cache", required=True)
    parser.add_argument("--session-state", required=True)
    parser.add_argument("--inject-cache", default="")
    args = parser.parse_args()

    raw = ""
    if args.hook_stdin_file and os.path.isfile(args.hook_stdin_file):
        with open(args.hook_stdin_file, "r", encoding="utf-8") as handle:
            raw = handle.read()
    else:
        raw = sys.stdin.read()
    try:
        hook_data = json.loads(raw) if raw.strip() else {}
    except Exception:
        hook_data = {}
    if not isinstance(hook_data, dict):
        hook_data = {}

    if args.mode == "claude-post-tool":
        hook_data = normalize_claude_post_tool(hook_data)
        mode = "atomic-file" if pick(hook_data, ["file_path"]) else "atomic-tool"
    elif args.mode == "turn-agent" and AGENT_TYPE == "claude_code":
        hook_data = normalize_claude_stop(hook_data)
        mode = "turn-agent"
    else:
        mode = args.mode

    if mode == "atomic-file":
        env = build_atomic_file(
            hook_data,
            prompt_cache_file=args.prompt_cache,
            session_state_file=args.session_state,
        )
    elif mode == "atomic-tool":
        env = build_atomic_tool(
            hook_data,
            prompt_cache_file=args.prompt_cache,
            session_state_file=args.session_state,
        )
    else:
        env = build_turn_agent(
            hook_data,
            prompt_cache_file=args.prompt_cache,
            session_state_file=args.session_state,
            inject_cache_file=args.inject_cache,
        )

    if not env:
        return 0
    print(json.dumps(env, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
