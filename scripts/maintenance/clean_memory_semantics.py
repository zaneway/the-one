#!/usr/bin/env python3
"""Clean semantic pollution in The One memory SQLite data.

Default mode is dry-run. Use --apply to mutate the database.
"""

from __future__ import annotations

import argparse
import json
import sqlite3
from dataclasses import dataclass
from pathlib import Path


NOISY_EXACT = {
    "claude 已完成本轮响应",
    "agent 已完成本轮响应",
    "codex 已完成本轮响应",
    "用户输入摘要未直接可见",
}
METADATA_KEYWORDS = {"hook", "turn-completed", "trace", "memory-context", "file-edit", "tool-result"}


@dataclass
class Change:
    table: str
    row_id: str
    field: str
    before: str
    after: str


def load_json_list(value: str | None) -> list[str]:
    if not value:
        return []
    try:
        data = json.loads(value)
    except Exception:
        return []
    if not isinstance(data, list):
        return []
    return [str(item).strip() for item in data if str(item).strip()]


def dump_json_list(values: list[str]) -> str:
    return json.dumps(values, ensure_ascii=False)


def normalize_text(value: str) -> str:
    text = (value or "").strip()
    for label in ("【结论/决策】", "【事件】", "【事实】", "【约束】", "【关联】", "【状态】"):
        text = text.replace(label, " ")
    return " ".join(text.split()).lower()


def is_metadata_keyword(value: str) -> bool:
    lower = value.strip().lower()
    return lower in METADATA_KEYWORDS or lower.startswith("trace:") or lower.startswith("mem:")


def is_low_value_text(value: str) -> bool:
    normalized = normalize_text(value)
    if not normalized:
        return True
    if normalized in NOISY_EXACT:
        return True
    if "已完成本轮响应" in normalized and len(normalized) <= 24:
        return True
    return any(marker in normalized for marker in ("完整任务提示词", "可直接复制给", "```", "# 任务："))


def clean_keywords(value: str | None) -> str:
    out: list[str] = []
    seen: set[str] = set()
    for item in load_json_list(value):
        if is_metadata_keyword(item) or item in seen:
            continue
        seen.add(item)
        out.append(item)
    return dump_json_list(out)


def clean_spans(value: str | None, content_summary: str = "", actor: str = "") -> str:
    out: list[str] = []
    seen: set[str] = set()
    summary_norm = normalize_text(content_summary)
    for item in load_json_list(value):
        item_norm = normalize_text(item)
        if is_low_value_text(item):
            continue
        if actor == "user" and summary_norm and item_norm not in summary_norm:
            continue
        if item in seen:
            continue
        seen.add(item)
        out.append(item)
    return dump_json_list(out)


def concise_title(memory_type: str, content: str, max_chars: int = 96) -> str:
    text = (content or "").strip()
    for sep in ("\n", "。", "！", "？", ". ", "! ", "? "):
        idx = text.find(sep)
        if idx > 0:
            text = text[: idx if sep == "\n" else idx + len(sep)].strip()
            break
    if len(text) > max_chars:
        text = text[:max_chars].rstrip() + "..."
    return f"{memory_type}: {text}" if text else memory_type


def build_search_text(
    title: str,
    content: str,
    normalized_content: str,
    keywords_json: str,
    tags_json: str,
    retrieval_cues_json: str,
    entities_json: str,
) -> str:
    parts = [
        title,
        content,
        normalized_content,
        labeled("keywords", load_json_list(keywords_json)),
        labeled("tags", load_json_list(tags_json)),
        labeled("retrieval", load_json_list(retrieval_cues_json)),
        labeled("entities", load_json_list(entities_json)),
    ]
    out: list[str] = []
    seen: set[str] = set()
    for part in parts:
        part = (part or "").strip()
        key = " ".join(part.split())
        if not part or key in seen:
            continue
        seen.add(key)
        out.append(part)
    return "\n".join(out).strip()


def labeled(label: str, values: list[str]) -> str:
    text = " ".join(values).strip()
    return f"{label}: {text}" if text else ""


def collect_changes(conn: sqlite3.Connection) -> tuple[list[Change], list[str]]:
    changes: list[Change] = []
    archive_memory_ids: list[str] = []

    for row in conn.execute(
        "select id, actor, content_summary, keywords_json, salient_spans_json from raw_event"
    ):
        row_id, actor, summary, keywords, spans = row
        new_keywords = clean_keywords(keywords)
        if should_update_json_list(keywords, new_keywords):
            changes.append(Change("raw_event", row_id, "keywords_json", keywords or "", new_keywords))
        new_spans = clean_spans(spans, summary or "", actor or "")
        if should_update_json_list(spans, new_spans):
            changes.append(Change("raw_event", row_id, "salient_spans_json", spans or "", new_spans))

    for row in conn.execute("select id, interpreted_statement, keywords_json, salient_spans_json from evidence"):
        row_id, statement, keywords, spans = row
        new_keywords = clean_keywords(keywords)
        if should_update_json_list(keywords, new_keywords):
            changes.append(Change("evidence", row_id, "keywords_json", keywords or "", new_keywords))
        new_spans = clean_spans(spans, statement or "", "")
        if should_update_json_list(spans, new_spans):
            changes.append(Change("evidence", row_id, "salient_spans_json", spans or "", new_spans))

    query = """select id, memory_type, title, content, normalized_content, keywords_json,
        tags_json, retrieval_cues_json, entities_json, search_text, state
        from memory_item"""
    for row in conn.execute(query):
        (
            row_id,
            memory_type,
            title,
            content,
            normalized,
            keywords,
            tags,
            cues,
            entities,
            search_text,
            state,
        ) = row
        if is_low_value_text(content or "") and state != "archived":
            archive_memory_ids.append(row_id)
            changes.append(Change("memory_item", row_id, "state", state or "", "archived"))
        new_keywords = clean_keywords(keywords)
        if should_update_json_list(keywords, new_keywords):
            changes.append(Change("memory_item", row_id, "keywords_json", keywords or "", new_keywords))
        new_title = concise_title(memory_type or "memory", content or "")
        if title != new_title:
            changes.append(Change("memory_item", row_id, "title", title or "", new_title))
        new_search = build_search_text(new_title, content or "", normalized or "", new_keywords, tags or "", cues or "", entities or "")
        if (search_text or "") != new_search:
            changes.append(Change("memory_item", row_id, "search_text", search_text or "", new_search))

    return changes, archive_memory_ids


def should_update_json_list(before: str | None, after: str) -> bool:
    original = (before or "").strip()
    if original == "" and after == "[]":
        return False
    return original != after


def apply_changes(conn: sqlite3.Connection, changes: list[Change], archive_memory_ids: list[str]) -> None:
    for change in changes:
        conn.execute(f"update {change.table} set {change.field} = ? where id = ?", (change.after, change.row_id))
    for memory_id in archive_memory_ids:
        conn.execute("delete from memory_item_fts where memory_id = ?", (memory_id,))
    for row in conn.execute("select id, search_text, state from memory_item where state in ('stable','pending_review','provisional')"):
        memory_id, search_text, _state = row
        conn.execute("delete from memory_item_fts where memory_id = ?", (memory_id,))
        conn.execute("insert into memory_item_fts(memory_id, search_text) values (?, ?)", (memory_id, search_text or ""))
    conn.commit()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--db", default="./.theone-data/memory.db")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--sample", type=int, default=8)
    args = parser.parse_args()

    db_path = Path(args.db).expanduser()
    conn = sqlite3.connect(str(db_path))
    changes, archive_ids = collect_changes(conn)
    print(f"db={db_path}")
    print(f"mode={'apply' if args.apply else 'dry-run'}")
    print(f"changes={len(changes)}")
    print(f"archive_memory_items={len(archive_ids)}")
    for change in changes[: args.sample]:
        before = change.before.replace("\n", " ")[:120]
        after = change.after.replace("\n", " ")[:120]
        print(f"- {change.table}.{change.field} id={change.row_id}: {before!r} -> {after!r}")
    if args.apply:
        apply_changes(conn, changes, archive_ids)
        print("applied=true")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
