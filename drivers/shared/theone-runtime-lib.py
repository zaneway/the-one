#!/usr/bin/env python3
"""Shared helpers for theone Agent hook scripts (Cursor / Claude Code)."""

from __future__ import annotations

import hashlib
import json
import os
import re
from typing import Any


def pick(dct: dict[str, Any], keys: list[str], default: str = "") -> str:
    for key in keys:
        value = dct.get(key, "")
        if isinstance(value, str) and value.strip():
            return value.strip()
    return default


def load_json(path: str) -> dict[str, Any]:
    if not path or not os.path.isfile(path):
        return {}
    try:
        with open(path, "r", encoding="utf-8") as handle:
            data = json.load(handle)
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}


def binding_state_file(state_dir: str, agent_type: str = "cursor") -> str:
    safe = (agent_type or "cursor").strip() or "cursor"
    return os.path.join(state_dir, f"binding.{safe}.json")


def resolve_binding_path(session_state_file: str, agent_type: str = "cursor") -> str:
    if session_state_file and session_state_file.endswith("session.json"):
        return binding_state_file(os.path.dirname(session_state_file), agent_type)
    if session_state_file and "binding." in os.path.basename(session_state_file):
        return session_state_file
    if session_state_file:
        return session_state_file
    return binding_state_file(".", agent_type)


def runtime_cache_name(base: str, agent_type: str) -> str:
    at = (agent_type or "cursor").strip() or "cursor"
    if at == "cursor":
        return base
    ext = os.path.splitext(base)[1]
    stem = base[: -len(ext)] if ext else base
    return f"{stem}.{at}{ext}"


def resolve_session_task(
    hook_data: dict[str, Any],
    *,
    prompt_cache_file: str = "",
    session_state_file: str = "",
    agent_type: str = "cursor",
) -> tuple[str, str]:
    binding_path = resolve_binding_path(session_state_file, agent_type)
    binding = load_json(binding_path)
    prompt_cache = load_json(prompt_cache_file)
    session_id = pick(
        hook_data,
        ["session_id", "sessionId", "conversation_id", "conversationId"],
        pick(prompt_cache, ["session_id"], pick(binding, ["session_id"], "")),
    )
    task_id = pick(
        hook_data,
        ["task_id", "taskId"],
        pick(prompt_cache, ["task_id"], pick(binding, ["task_id"], f"task_{agent_type}_auto")),
    )
    if not task_id:
        task_id = f"task_{agent_type}_auto"
    return session_id, task_id


def load_inject_cache(path: str) -> dict[str, Any]:
    return load_json(path)


def merge_inject_fields(
    payload: dict[str, Any],
    inject_cache: dict[str, Any],
    *,
    prompt_cache: dict[str, Any] | None = None,
) -> dict[str, Any]:
    if not inject_cache:
        return payload
    if prompt_cache:
        gen = pick(prompt_cache, ["generation_id", "generationId"])
        cache_gen = pick(inject_cache, ["generation_id", "generationId"])
        if gen and cache_gen and gen != cache_gen:
            return payload
        if not gen and not cache_gen:
            fp = pick(prompt_cache, ["prompt_fingerprint"])
            cache_fp = pick(inject_cache, ["prompt_fingerprint"])
            if fp and cache_fp and fp != cache_fp:
                return payload
    trace_id = pick(inject_cache, ["retrieval_trace_id"])
    used_ids = inject_cache.get("used_memory_ids") or []
    if not isinstance(used_ids, list):
        used_ids = []
    used_ids = [str(item).strip() for item in used_ids if str(item).strip()]
    injected = bool(inject_cache.get("injected_to_prompt"))
    if trace_id:
        payload["retrieval_trace_id"] = trace_id
    if used_ids:
        payload["used_memory_ids"] = used_ids
    if injected:
        payload["injected_to_prompt"] = True
    return payload


def prompt_fingerprint(text: str) -> str:
    normalized = " ".join((text or "").split())[:1000]
    return hashlib.sha1(normalized.encode("utf-8")).hexdigest()[:16]


def build_turn_id(*parts: str) -> str:
    base = "|".join(parts)
    return "turn_" + hashlib.sha1(base.encode("utf-8")).hexdigest()[:16]


def _clean_text(text: str) -> str:
    value = str(text or "").replace("```", " ")
    return re.sub(r"\s+", " ", value).strip()


def _clip(text: str, max_chars: int) -> str:
    value = _clean_text(text)
    if max_chars <= 0 or len(value) <= max_chars:
        return value
    return value[:max_chars].rstrip()


def facts_from_text(text: str, max_items: int = 2) -> list[str]:
    """Extract bounded atomic fact-like spans with no LLM call."""
    value = _clean_text(text)
    if not value:
        return []
    parts = re.split(r"(?<=[。！？.!?])\s+|[\r\n]+|[;；]", value)
    facts: list[str] = []
    for part in parts:
        item = _clip(part, 500)
        if not item:
            continue
        if len(item) < 8 and len(parts) > 1:
            continue
        facts.append(item)
        if len(facts) >= max_items:
            break
    if not facts:
        facts.append(_clip(value, 500))
    return facts[:max_items]


def keywords_from_text(text: str, max_items: int = 6) -> list[str]:
    """Extract bounded semantic-ish search anchors with no capture metadata."""
    value = _clean_text(text)
    if not value:
        return []
    candidates = re.findall(r"[A-Za-z][A-Za-z0-9_./:-]{2,}|[\u4e00-\u9fff]{2,12}", value)
    out: list[str] = []
    seen: set[str] = set()
    blocked = {"hook", "turn-completed", "cursor", "claude_code"}
    for item in candidates:
        token = item.strip(".,;:()[]{}<>`\"'")
        lower = token.lower()
        if not token or lower in blocked or lower.startswith("trace:") or lower.startswith("mem:"):
            continue
        if token in seen:
            continue
        seen.add(token)
        out.append(token)
        if len(out) >= max_items:
            break
    return out


CURRENT_DIRECTORY_KEYS = (
    "cwd",
    "current_working_directory",
    "currentWorkingDirectory",
    "working_directory",
    "workingDirectory",
    "workspace_dir",
    "workspaceDir",
)


def _dir_basename(value: str) -> str:
    value = str(value or "").strip()
    if not value:
        return ""
    name = os.path.basename(os.path.abspath(value))
    if name and name not in {".", os.sep}:
        return name
    return ""


def _project_dir_name(source: dict[str, Any] | None = None) -> str:
    source = source if isinstance(source, dict) else {}
    for key in CURRENT_DIRECTORY_KEYS:
        if name := _dir_basename(source.get(key, "")):
            return name
    name = os.path.basename(os.getcwd())
    if name:
        return name
    for key in ("THEONE_PROJECT_DIR", "ROOT_DIR"):
        if name := _dir_basename(os.environ.get(key, "")):
            return name
    value = os.environ.get("PWD", "").strip()
    if name := _dir_basename(value):
        return name
    return ""


def _normalize_project_scope_id(value: str) -> str:
    value = value.strip()
    if value == "the-one":
        return "theone"
    return value


def project_scope_ids(source: dict[str, Any] | None = None) -> tuple[str, str]:
    project_id = _normalize_project_scope_id(
        os.environ.get("THEONE_PROJECT_ID", "").strip() or _project_dir_name(source)
    )
    repo_id = _normalize_project_scope_id(os.environ.get("THEONE_REPO_ID", "").strip() or project_id)
    return project_id, repo_id


def format_structured_content_summary(
    event_type: str,
    primary: str,
    *,
    fact_text: str = "",
    conclusion_text: str = "",
    constraint_text: str = "",
    relation_text: str = "",
    status_text: str = "",
    max_chars: int = 800,
    max_spans: int = 3,
) -> tuple[str, list[str]]:
    """Build a structured index-card content_summary and salient_spans."""
    event_type = _clean_text(event_type)
    primary = _clean_text(primary)
    fact_text = _clean_text(fact_text)

    if "【事件】" in primary or "【事实】" in primary or "【结论" in primary:
        spans = facts_from_text(fact_text or primary, max_spans)
        return _clip(primary, max_chars), spans

    lines: list[str] = []
    conclusion_events = {"agent.response.summary", "task.result", "session.end", "agent.decision"}
    if not conclusion_text and event_type in conclusion_events:
        conclusion_text = primary
        primary = ""
    if not conclusion_text and not constraint_text and len(primary or fact_text) > 400:
        constraint_text = primary or fact_text
        primary = ""

    if conclusion_text:
        lines.append(f"【结论/决策】{_clip(conclusion_text, 300)}")
    if constraint_text:
        lines.append(f"【约束】{_clip(constraint_text, 300)}")
    if primary:
        lines.append(f"【事件】{_clip(primary, 300)}")

    facts = facts_from_text(fact_text or primary or conclusion_text or constraint_text, max_spans)
    for fact in facts:
        if fact and fact not in (primary, conclusion_text):
            lines.append(f"【事实】{_clip(fact, 300)}")
            break

    if relation_text:
        lines.append(f"【关联】{_clip(relation_text, 240)}")
    if status_text:
        lines.append(f"【状态】{_clip(status_text, 200)}")
    if not lines:
        lines.append(f"【事件】{event_type or 'agent hook event'}")

    summary = _clip("\n".join(lines), max_chars)
    return summary, facts[:max_spans]
