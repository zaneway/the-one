# Cursor Hook 模板（P5 薄 Driver）

**实现已迁移至仓库根目录：**

| 角色 | 路径 |
|------|------|
| 统一入口 | `drivers/cursor/entry.sh` |
| 各事件脚本 | `drivers/cursor/hooks/*.sh` |
| 共享逻辑 | `drivers/shared/theone-hook-*.py`、`theone-build-ingest.py` |
| 项目 Hook 配置 | `.cursor/hooks.json` → `drivers/cursor/entry.sh <event>` |

**注意**：Cursor 的 `stop` 在每轮回复结束时触发，**不要**配置为 `session.end`（会话结束仅用 `sessionEnd`）。可选 `drivers/cursor/hooks/theone-stop.sh`（no-op）。

本目录仅保留**兼容转发**（`exec` 到 `drivers/cursor/hooks/`），**禁止**在此新增内嵌 Python（`` python3 - <<'PY' ``）。

## v1 已废弃脚本

已移至 `doc/archive/v1-hook-scripts/`：

- `theone-build-turn.py` → `theone ingest` + `kind=turn.completed`
- `theone-inject-context.py` → `theone prefetch-context` + Surface 文件
