# The One Cursor 安装手册

本文面向 **使用发布包的 Cursor 用户**，不要求用户克隆源码、安装 Go 或执行 `make build`。

发布包已经包含：

| 路径 | 作用 |
|------|------|
| `bin/theone` | The One MCP server / ingest CLI |
| `install-cursor.sh` | 安装到目标项目的脚本 |
| `theone.yaml` | 默认运行配置 |
| `drivers/cursor/` | Cursor Hook 入口与事件脚本 |
| `drivers/shared/` | Hook 共享运行时 |
| `doc/cursor/` | Cursor 模板与说明 |


源码仓库维护者生成发布包使用：

```bash
make package-cursor VERSION=1.0.0
```

生成结果在 `dist/theone-cursor-<version>-<os>-<arch>.tar.gz`。

---

## 1. 用户安装流程

假设用户已经拿到并解压发布包：

```bash
tar -xzf theone-cursor-1.0.0-darwin-arm64.tar.gz
cd theone-cursor-1.0.0-darwin-arm64
./install-cursor.sh --project /path/to/your/project
```

如果已经在目标项目目录下，也可以：

```bash
/path/to/theone-cursor-1.0.0-darwin-arm64/install-cursor.sh
```

安装脚本会写入或更新目标项目：

| 目标路径 | 内容 |
|----------|------|
| `bin/theone` | 发布包中的二进制 |
| `theone.yaml` | 默认配置；已存在时默认保留 |
| `drivers/cursor/` | Cursor Hook 脚本 |
| `drivers/shared/` | Hook 共享脚本 |
| `.cursor/mcp.json` | MCP 配置，指向目标项目内的 `bin/theone` |
| `.cursor/hooks.json` | Cursor Hooks 配置 |
| `.cursor/rules/theone-memory-observe.mdc` | Agent 记忆规则 |
| `.cursor/rules/theone-injected-context.mdc` | prefetch 注入面初始文件 |
| `.theone-data/` | SQLite 数据库、日志、runtime state |

已有 `.cursor/mcp.json` / `.cursor/hooks.json` 时，脚本会在安装了 `jq` 的环境中自动合并；没有 `jq` 时会提示用户手工合并。

覆盖运行时文件：

```bash
./install-cursor.sh --project /path/to/your/project --force
```

---

## 2. Cursor 内启用

安装完成后：

1. 用 Cursor 打开目标项目根目录。
2. Reload Window 或重启 Cursor。
3. Settings -> MCP，确认 `theone` 已启用。
4. 调用 `memory_health`，应返回 `ok: true`。
5. 确认 Rules 中可见 The One 记忆规则。
6. 新开 Agent 对话，完成一轮有实质内容的问答。

---

## 3. 验收命令

在目标项目根目录执行：

```bash
bin/theone health -config ./theone.yaml -data-dir ./.theone-data
```

查看最近 raw_event：

```bash
sqlite3 .theone-data/memory.db \
  "SELECT event_type, occurred_at FROM raw_event ORDER BY occurred_at DESC LIMIT 10;"
```

当前策略下：

| 事件 | 是否进入 `raw_event` | 说明 |
|------|----------------------|------|
| `session.start` | 否 | Hook 保留，用于 session/task 绑定与诊断；不写 raw_event |
| `tool.result.summary` | 否 | Hook 保留，用于工具链路诊断；不写 raw_event |
| `file.edit.summary` | 是 | 文件修改摘要事件；保留路径、change_type、hash 和摘要，不保存完整 diff |
| `turn.completed` | 是 | 一轮用户请求 + Agent 回复 |
| `session.end` | 是 | 会话结束与清理 |
| Agent 主动 `memory_observe` | 是 | 高价值事实/结论由 Rule 引导写入 |

检查 binding：

```bash
test -f .theone-data/runtime-state/binding.cursor.json && echo "binding ok"
```

检查日志：

```bash
tail -n 50 .theone-data/logs/theone.log
```

日志中出现 `theone initialized`、`mcp tool called`、`observe completed` 属于正常信号。周期性的 stdio disconnect 多数是 Cursor 探测连接，不一定表示失败。

---

## 4. 能力边界

完整 Cursor 体验由三部分组成：

| 组件 | 作用 |
|------|------|
| MCP | `memory_health`、`memory_search`、`memory_context`、`memory_observe`、`memory_remember` |
| Hooks | 自动 prefetch、turn/file/session 事件捕获、session binding |
| Rules | 引导 Agent 在高价值时主动调用 `memory_observe` / `memory_remember` |

数据流：

```text
用户提交 prompt
  -> beforeSubmitPrompt: prefetch -> .cursor/rules/theone-injected-context.mdc
  -> Agent 推理
  -> afterAgentResponse: turn.completed -> raw_event
  -> afterFileEdit: file.edit.summary -> raw_event
  -> sessionStart / afterMCPExecution: 只做控制面处理，不写 raw_event
  -> 异步 Worker: raw_event -> evidence -> memory_candidate -> memory_item
```

---

## 5. 模板说明

`doc/cursor` 中的模板由安装脚本消费：

| 文件 | 作用 |
|------|------|
| `mcp.json` | MCP 配置模板，占位符 `__THEONE_REPO__` 会替换为目标项目绝对路径 |
| `hooks.json` | Cursor Hooks 配置模板 |
| `rule/theone-memory-observe.mdc` | Agent Rule |
| `context/theone-injected-context.mdc.template` | 初始注入面 |

`doc/adapters/cursor/mapping.yaml` 是维护者参考的事件映射说明，不要求普通用户阅读。

---

## 6. 排障

| 现象 | 可能原因 | 处理 |
|------|----------|------|
| Settings -> MCP 找不到 `theone` | `.cursor/mcp.json` 未写入或路径错误 | 重跑 `install-cursor.sh --project <项目>` |
| `memory_health` 失败 | `bin/theone` 不存在、不可执行或配置错误 | 检查 `bin/theone`、`theone.yaml`、`.theone-data/logs/theone.log` |
| `readonly database` | 数据目录不可写 | 确认 MCP 参数中 `-data-dir` 指向项目内 `.theone-data` |
| Rules 不生效 | 没用项目根目录打开 Cursor | 用目标项目根目录重新打开并 Reload Window |
| raw_event 为空 | Hooks 未启用、Agent 未产生有效事件、或只触发了被抑制事件 | 完成一轮有内容的 Agent 回复，再查 `turn.completed` |
| 看不到 `session.start` / `tool.result.summary` | 当前策略就是不写入 raw_event | 查 binding 和日志，不把它作为 raw_event 验收项 |
| prefetch 无注入 | 尚无可召回记忆或 binding 未建立 | 先完成一轮高价值对话并让 Agent 调用 `memory_observe` / `memory_remember` |
| 换目录后失效 | `.cursor/mcp.json` 里仍是旧绝对路径 | 重跑安装脚本 |

---

## 7. 维护者发布清单

发布前至少验证：

```bash
scripts/acceptance/p5_thin_driver.sh
python3 -m json.tool doc/cursor/mcp.json >/dev/null
python3 -m json.tool doc/cursor/hooks.json >/dev/null
python3 -m unittest \
  drivers/shared/theone-build-ingest_test.py \
  drivers/shared/theone-hook-prefetch_test.py \
  drivers/shared/theone-hook-session_test.py
make package-cursor VERSION=<version>
```


