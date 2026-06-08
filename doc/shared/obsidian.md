# Markdown 文件关系图谱：核心逻辑与实现方式

> 本文档说明：**单个 `.md` 文件**如何与 Vault / 知识库中的其他实体建立关系，并如何从这些关系派生出可查询、可可视化的图谱。  
> 以 Obsidian 为参考实现，同时给出可脱离 Obsidian 独立复现的通用方案。

---

## 1. 核心观念

关系图谱**不是**单独存储的二进制图数据库，而是从 Markdown（及关联文件）**解析派生**的结构：

```
.md 文件（真相源）
    ↓ 解析
链接 / 标签 / 属性 / 嵌入
    ↓ 解析到目标
节点 ID + 边（关系）
    ↓ 索引（可选持久化）
邻接表 / 反向索引
    ↓ 查询 + 布局
局部子图 或 全局图谱
```

**单文件进入图谱的最小条件**：文件中存在至少一条可被解析的**出站关系**（wikilink、Markdown 链接、frontmatter 链接、标签层级等），或**被其他文件链接**（入站关系）。二者缺一则为**孤立节点（orphan）**。

---

## 2. 单文件视角：出站与入站

以文件 `Projects/the-one.md` 为例：

```markdown
---
title: The One
tags: [memory, graph]
aliases: [记忆系统]
related: "[[Agent 接入层与 Hook 设计]]"
---

# 架构

参见 [[memory_observe]] 与 [[doc/shared/content-summary-structured]]。

Also see [Obsidian 帮助](https://help.obsidian.md/data-storage)  # 外链，通常不进内部图谱

#memory #graph
```

### 2.1 从该文件派生的出站边

| 关系类型 | 源 | 目标 | 解析依据 |
|----------|----|------|----------|
| wikilink | `Projects/the-one.md` | `memory_observe`（待 resolve） | 正文 `[[memory_observe]]` |
| wikilink | 同上 | `doc/shared/content-summary-structured` | 正文 `[[doc/shared/...]]` |
| frontmatter 链接 | 同上 | `Agent 接入层与 Hook 设计` | `related: "[[...]]"` |
| tag | 同上 | `#memory` | 正文 / frontmatter tags |
| tag | 同上 | `#graph` | 同上 |
| alias 反向 | 其他文件链 `[[记忆系统]]` | 本文件 | 本文件 `aliases` 注册 |

### 2.2 入站边（backlinks）

其他任意文件 `X.md` 若含 `[[the-one]]` 或 `[[Projects/the-one]]` 或匹配 alias `记忆系统`，则产生边：

```
X.md ──wikilink──▶ Projects/the-one.md
```

**入站边不写在当前文件内**，需通过全局索引（遍历所有文件的 `resolvedLinks`）或反向索引表获得。

### 2.3 单文件局部子图（Local Graph）

以当前文件为根、深度 `d` 的 BFS：

```
depth=0: Projects/the-one.md
depth=1: memory_observe, content-summary-structured, Agent 接入层..., #memory, #graph
depth=2: 上述节点各自的邻居
```

Obsidian Local Graph 即此模型；全局 Graph 则是 Vault 内全部节点与边的并集。

---

## 3. 关系的数据来源

### 3.1 Markdown 正文

| 语法 | 示例 | 边类型 | 是否参与内部图谱 |
|------|------|--------|------------------|
| Wikilink | `[[Note]]` | 文件 → 文件 | 是 |
| Wikilink + 别名 | `[[真实名\|显示]]` | 文件 → 文件 | 是（目标为 path 部分） |
| 标题锚点 | `[[Note#Section]]` | 文件 → 文件（可带 subpath） | 是（节点级或子图级） |
| 块引用 | `[[Note#^block-id]]` | 文件 → 块 | 可选（Obsidian 块级图谱扩展） |
| 嵌入 | `![[Note]]` | embed 关系 | 可选（Graph 可开关） |
| Markdown 链接 | `[t](path.md)` | 文件 → 文件 | 是（需 URL 解码） |
| 标签 | `#tag/sub` | 文件 → 标签节点 | 可选 |
| 外链 | `[t](https://...)` | — | 通常否 |

### 3.2 YAML Frontmatter（Properties）

```yaml
---
tags: [a, b]
aliases: [别名A]
see_also: "[[Other Note]]"
---
```

- `tags` → 文件与标签节点的边
- `aliases` → 注册别名索引，供链接解析
- 任意含 `"[[...]]"` 的字段 → `frontmatterLinks`

### 3.3 关联文件格式

| 格式 | 关系来源 |
|------|----------|
| `.canvas` | `type:file` 节点的 `file` 字段；`edges` 数组 |
| 附件 | `![[image.png]]` 嵌入边（可选展示为节点） |

---

## 4. 核心处理流水线

### 4.1 阶段概览

```
┌─────────────┐   ┌──────────────┐   ┌───────────────┐   ┌─────────────┐
│ 1. 扫描     │ → │ 2. 逐文件解析 │ → │ 3. 链接 resolve │ → │ 4. 建图索引 │
│ 枚举 .md 等 │   │ 提取 links   │   │ path → 文件路径 │   │ 邻接表      │
└─────────────┘   └──────────────┘   └───────────────┘   └─────────────┘
                                                              ↓
                                                    ┌─────────────────┐
                                                    │ 5. 查询 / 布局  │
                                                    │ 局部子图 / 全局 │
                                                    └─────────────────┘
```

### 4.2 阶段 1：扫描（Scan）

- 递归遍历 Vault 根目录
- 应用 ignore 规则（如 `.obsidian/`、`.git/`、用户排除模式）
- 产出文件列表：`[{ path, mtime, hash }]`

### 4.3 阶段 2：解析（Parse）

对每个 `.md` 文件，**不执行链接 resolve**，只提取原始引用：

**输出结构（单文件 ParseResult）**：

```typescript
interface ParseResult {
  file_id: string;           // 如 "Projects/the-one"（.md 省略扩展名）
  path: string;              // Vault 内绝对路径
  frontmatter: Record<string, unknown>;
  wikilinks: Array<{ text: string; line: number; col: number }>;
  embeds: Array<{ text: string; line: number; col: number }>;
  tags: Array<{ name: string; line: number }>;
  markdown_links: Array<{ text: string; href: string; line: number }>;
}
```

**解析规则要点**：

1. 分离 YAML frontmatter（`---` 包裹）
2. 跳过代码块（`` ``` ``）与 HTML 块内的伪链接
3. Wikilink 正则（简化）：`\[\[([^\]|#]+)(?:#([^\]|]+))?(?:\|([^\]]+))?\]\]`
4. 标签：`#` 后接合法 tag 字符
5. 记录**行号/列号**，便于 UI 跳转与增量更新

### 4.4 阶段 3：链接 Resolve

将 `linktext`（如 `folder/note#Heading`）映射为 Vault 内**唯一目标文件**。

**拆分**：

```typescript
parseLinktext("folder/note#Heading|Display")
// → { path: "folder/note", subpath: "#Heading" }
```

**Resolve 算法（Obsidian 默认：Shortest path when possible）**：

```
输入: linkpath, sourcePath（发出链接的文件路径）
输出: targetPath | null（未解析则进 unresolvedLinks）

1. 若 linkpath 含扩展名且路径存在 → 直接命中
2. 否则在 Vault 内搜索 basename 匹配 linkpath（忽略 .md）
3. 若唯一匹配 → 返回
4. 若多个匹配 → 计算各候选与 sourcePath 的路径距离，取最短
5. 若无文件匹配 → 查 aliases 索引
6. 仍无 → unresolved（图谱可显示虚节点）
```

**Aliases 索引**（启动时构建）：

```
alias "记忆系统" → Projects/the-one.md
```

### 4.5 阶段 4：建图索引

**正向邻接表（resolvedLinks）**：

```typescript
// sourcePath → { destPath → count }
resolvedLinks: Record<string, Record<string, number>>
```

**反向索引（backlinks，派生）**：

```typescript
// destPath → Set<sourcePath>
backlinks: Record<string, Set<string>>
```

**标签索引**：

```typescript
// tagName → Set<filePath>
tagIndex: Record<string, Set<string>>
```

**单文件增量更新**：文件变更时只重解析该 path，更新其在 `resolvedLinks` 中的出边，并修正所有受影响节点的入边。

### 4.6 阶段 5：图谱查询与布局

**查询 API**：

| 操作 | 输入 | 输出 |
|------|------|------|
| 全局图 | filters | `{ nodes, links }` |
| 局部图 | rootPath, depth | 子图 |
| 邻居 | path, direction | 相邻节点列表 |
| Orphans | — | 无任何边的节点 |

**布局**：力导向（Force-Directed Layout）

- **Center**：向画布中心聚拢
- **Repel**：无关节点互斥（∝ 1/d²）
- **Link**：有边节点弹性连接
- **Link distance**：边目标长度

Obsidian 现用 PixiJS 渲染 + 自研/内嵌力模拟；独立实现可用 D3 `d3-force` 或 Cytoscape.js。

---

## 5. 图谱数据结构（通用）

### 5.1 节点（Node）

```json
{
  "id": "Projects/the-one",
  "type": "file",
  "label": "the-one",
  "path": "Projects/the-one.md",
  "inDegree": 3,
  "outDegree": 5,
  "tags": ["memory", "graph"],
  "orphan": false
}
```

可选节点类型：`file` | `tag` | `attachment` | `unresolved`

### 5.2 边（Link / Edge）

```json
{
  "source": "Projects/the-one",
  "target": "doc/shared/content-summary-structured",
  "relation": "wikilink",
  "count": 1,
  "subpath": null
}
```

`relation` 枚举建议：`wikilink` | `markdown_link` | `embed` | `tag` | `frontmatter_link` | `canvas_edge` | `parent`（标签层级）

### 5.3 D3 兼容输出示例

```json
{
  "nodes": [
    { "id": "note-a", "type": "file" },
    { "id": "note-b", "type": "file" },
    { "id": "#python", "type": "tag" }
  ],
  "links": [
    { "source": "note-a", "target": "note-b", "relation": "wikilink" },
    { "source": "note-a", "target": "#python", "relation": "tag" }
  ]
}
```

---

## 6. 实现方式对照

### 6.1 Obsidian（参考实现）

| 层 | 实现 |
|----|------|
| 真相源 | Vault 内 `.md` / `.canvas` 纯文本 |
| 解析 | 内置 Markdown 解析器 + MetadataCache |
| 索引 | 内存 MetadataCache + IndexedDB 持久化 |
| 链接图 | `resolvedLinks` / `unresolvedLinks` |
| 可视化 | Graph View 核心插件，PixiJS + 力导向 |
| 单文件入口 | Local Graph（BFS by depth） |

关键 API（插件）：

- `metadataCache.getFileCache(file)` → 单文件出站 links/tags/blocks
- `metadataCache.resolvedLinks` → 全局正向边
- `metadataCache.getFirstLinkpathDest(linkpath, sourcePath)` → resolve

### 6.2 独立最小实现（伪代码）

```python
def build_graph(vault_root: Path) -> Graph:
    graph = Graph()
    alias_index = {}

    # Pass 1: parse all files
    parsed = {}
    for path in scan_md_files(vault_root):
        pr = parse_markdown(path)
        parsed[path] = pr
        for alias in pr.frontmatter.get("aliases", []):
            alias_index[alias] = path

    # Pass 2: resolve links → edges
    for source_path, pr in parsed.items():
        graph.add_node(source_path, type="file")
        for wl in pr.wikilinks:
            dest = resolve_link(wl.text, source_path, parsed, alias_index)
            if dest:
                graph.add_edge(source_path, dest, relation="wikilink")
            else:
                graph.add_node(wl.text, type="unresolved")
                graph.add_edge(source_path, wl.text, relation="wikilink")
        for tag in pr.tags:
            graph.add_node(f"#{tag}", type="tag")
            graph.add_edge(source_path, f"#{tag}", relation="tag")

    return graph


def local_subgraph(graph: Graph, root: str, depth: int) -> Graph:
    visited = {root}
    frontier = {root}
    for _ in range(depth):
        next_frontier = set()
        for node in frontier:
            for neighbor in graph.neighbors(node):
                if neighbor not in visited:
                    visited.add(neighbor)
                    next_frontier.add(neighbor)
        frontier = next_frontier
    return graph.induced_subgraph(visited)
```

### 6.3 开源参考

| 项目 | 用途 |
|------|------|
| [obsidian-parse](https://github.com/agent-hanju/obsidian-parse) | 解析 wikilink/embed/tag → D3 图 |
| [jsoncanvas](https://jsoncanvas.org/) | `.canvas` 格式规范 |
| [obsidian-api](https://github.com/obsidianmd/obsidian-api) | MetadataCache / CachedMetadata 类型 |

---

## 7. 单文件场景的常见模式

### 7.1 仅有一个 `.md`，无其他文件

- 若文中无 wikilink / tag → **孤立节点**，图谱仅 1 个节点 0 条边
- 若有 `#tag` → 可产生文件—标签边；标签可跨文件汇聚（需其他文件也打同 tag）

### 7.2 单文件 + 未创建的链接

`[[尚未写的笔记]]` → 进入 `unresolvedLinks`，Graph 可显示**虚节点**；创建同名文件后自动 resolve。

### 7.3 单文件 + 外部知识库

将其他系统的实体 ID 写入 frontmatter，用约定语法模拟边：

```yaml
---
theone_session: sess_cursor_20260608_graph
theone_task: task_md_graph_doc
refs:
  - "[[doc/shared/content-summary-structured]]"
---
```

解析器把 `refs` 与正文 wikilink **同等对待**，即可与仓库内其他 `.md` 连成图。

### 7.4 增量：只更新一个文件

```
1. 读取旧 ParseResult / 旧出边列表
2. 重新 parse 该文件
3. 从 resolvedLinks 删除该 source 的旧边
4. 写入新边
5. 更新 backlinks 中受影响的 dest
6. 若布局缓存依赖度数，标记相关节点 dirty
```

---

## 8. 过滤、分组与配置

Graph 展示前通常应用过滤器（Obsidian 存于 `.obsidian/graph.json`）：

| 配置项 | 作用 |
|--------|------|
| `search` | 搜索语法过滤节点（path、tag、text） |
| `showTags` | 是否展示标签节点 |
| `showAttachments` | 是否展示附件节点 |
| `hideUnresolved` | 隐藏未解析链接 |
| `showOrphans` | 是否展示孤立节点 |
| `colorGroups` | 按搜索条件分组着色 |
| `centerStrength` / `repelStrength` / `linkStrength` / `linkDistance` | 力导向参数 |

---

## 9. 与 The One 的映射（可选借鉴）

| Obsidian 概念 | The One 可能对应 |
|---------------|------------------|
| `.md` 文件 | `raw_event` / 文档 artifact |
| wikilink | `source_refs`、`content_summary` 中 `【关联】`、code path |
| MetadataCache | ingest 后的 SQLite + BM25 / 向量索引 |
| resolvedLinks | 实体—实体引用表或图边表 |
| Local Graph | 以 `session_id` / `task_id` / 单文件为中心的子图检索 |
| unresolved | 尚未 ingest 或无法 resolve 的引用 |

The One 若要为**单个 Markdown 文档**生成关系图，最小路径是：

1. 解析该 MD 的出站引用（路径、会话 ID、模块名）
2. 在索引中 resolve 到已有 `raw_event` 或 repo 内文件
3. 反向查 backlinks（谁引用了该文档/路径）
4. 输出 `{ nodes, links }` 供 UI 或 Agent 上下文使用

---

## 10. 验收清单（独立实现）

- [ ] 单文件 wikilink 正确解析为出站边
- [ ] frontmatter 内 `"[[link]]"` 被识别
- [ ] 代码块内 `[[fake]]` 不被误解析
- [ ] 同名文件按 sourcePath 最短路径 resolve
- [ ] aliases 可 resolve
- [ ] 未存在目标进入 unresolved
- [ ] backlinks 与 resolvedLinks 一致
- [ ] 单文件变更后增量更新，不全量重建
- [ ] local subgraph(depth=N) 节点数符合 BFS 预期
- [ ] 标签节点与 tag 边可选开关

---

## 参考

- [How Obsidian stores data](https://help.obsidian.md/data-storage)
- [Graph view](https://help.obsidian.md/plugins/graph)
- [Properties (YAML frontmatter)](https://help.obsidian.md/properties)
- [MetadataCache.resolvedLinks](https://docs.obsidian.md/Reference/TypeScript+API/MetadataCache/resolvedLinks)
- [JSON Canvas spec](https://jsoncanvas.org/)
- [obsidian-parse](https://github.com/agent-hanju/obsidian-parse)
