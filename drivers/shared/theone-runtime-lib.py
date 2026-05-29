#!/usr/bin/env python3
"""Shared helpers for theone Agent hook scripts (Cursor / Claude Code)."""

from __future__ import annotations

import hashlib
import json
import os
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
    keywords = list(payload.get("keywords") or [])
    for memory_id in used_ids[:6]:
        token = f"mem:{memory_id}"
        if token not in keywords:
            keywords.append(token)
    if trace_id and f"trace:{trace_id}" not in keywords:
        keywords.append(f"trace:{trace_id}")
    if keywords:
        payload["keywords"] = keywords
    return payload


def prompt_fingerprint(text: str) -> str:
    normalized = " ".join((text or "").split())[:1000]
    return hashlib.sha1(normalized.encode("utf-8")).hexdigest()[:16]


def build_turn_id(*parts: str) -> str:
    base = "|".join(parts)
    return "turn_" + hashlib.sha1(base.encode("utf-8")).hexdigest()[:16]
