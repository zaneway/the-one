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

> 内部链接的完整语法、链接模式、反链与 API 见 **§3.4 内部链接（专章）**。下表为速查。

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

### 3.4 内部链接（专章）

内部链接（Internal links）是 Obsidian 关系图谱的**主骨架**：图谱中的连线默认即「笔记 A 内部链接指向笔记 B」。本节集中说明语法、链接方式、解析接口与反链机制；§3.1 表格为速查，此处为完整规范。

#### 3.4.1 两种链接格式

Obsidian 支持两种互操作的内部链接写法，可在 **Settings → Files and links → Use `[[Wikilinks]]`** 切换默认生成格式：

| 格式 | 写法 | 特点 |
|------|------|------|
| **Wikilink**（默认） | `[[Three laws of motion]]` 或 `[[Three laws of motion.md]]` | 紧凑；重命名文件时可自动更新 vault 内引用 |
| **Markdown** | `[Three laws of motion](Three%20laws%20of%20motion.md)` | 通用 Markdown；空格等须 URL 编码（`%20`） |

说明：

- 关闭 Wikilink 后，输入 `[[` 触发补全，选中后仍会生成 **Markdown 链接**（Obsidian 自动处理编码）。
- 两种格式解析后均进入 `resolvedLinks`，**均参与图谱与反链**（前提是 resolve 成功）。
- 非 Markdown 文件（图片、PDF 等）作目标时，链接**必须带扩展名**，如 `[[Figure 1.png]]`。

#### 3.4.2 语法全集

**基础链接**

| 语法 | 示例 | 解析结果 |
|------|------|----------|
| 基础 wikilink | `[[Note]]` | path=`Note` |
| 带扩展名 | `[[Note.md]]` | path=`Note.md`（resolve 时通常去 `.md`） |
| 带路径 | `[[folder/sub/Note]]` | path=`folder/sub/Note` |
| 显示别名 | `[[真实文件名\|显示文字]]` | path=`真实文件名`（`\|` 前为解析目标） |
| Markdown 内部链 | `[显示](path/to.md)` | href 解码后作 path |

**子路径（subpath）**

| 语法 | 示例 | 说明 |
|------|------|------|
| 标题锚点 | `[[Note#Heading]]` | 指向目标笔记内某标题；图谱通常仍连到**文件节点** |
| 块引用 | `[[Note#^block-id]]` | Obsidian 专有；块 ID 由 `^id` 标记；站外工具常忽略 |
| Markdown 子路径 | `[节名](Note.md#Details)` | 同 wikilink 子路径语义 |

**嵌入（embed）**

| 语法 | 示例 | 图谱关系 |
|------|------|----------|
| 嵌入笔记 | `![[Note]]` | `embed` 边；Graph 设置中可开关 |
| 嵌入附件 | `![[image.png]]` | 可选展示附件节点 |

**Frontmatter 内链接**

Properties 中 internal link 须用引号包裹：

```yaml
---
related: "[[Other Note]]"
refs:
  - "[[doc/shared/content-summary-structured]]"
---
```

解析为 `frontmatterLinks`，与正文 wikilink **同等参与** resolve 与建边。

**不计入内部图谱**

| 类型 | 示例 | 原因 |
|------|------|------|
| 外链 | `[t](https://example.com)` | 目标在 vault 外 |
| Obsidian URI | `obsidian://open?vault=...&file=...` | 深链协议，非 vault 内路径引用 |
| 代码块内伪链接 | `` ``` `` 中的 `[[fake]]` | 解析器跳过 fenced code |

#### 3.4.3 链接路径解析模式

**Settings → Files and links → New link format** 决定「新建链接」的默认路径写法，并影响 resolve 时的查找策略：

| 模式 | 含义 | resolve 行为要点 |
|------|------|------------------|
| **Shortest path when possible**（默认） | 尽量用最短相对路径 | 同名多文件时，结合 **sourcePath** 选路径距离最短者 |
| **Relative path to vault** | 相对 vault 根目录 | 按相对路径逐级匹配 |
| **Absolute path in vault** | vault 内绝对路径 | 从根目录完整路径匹配 |

**Resolve 核心 API**（插件 / 独立实现均应对齐）：

```typescript
// 拆分 linktext
parseLinktext("folder/note#Heading|Display")
// → { path: "folder/note", subpath: "#Heading" }

getLinkpath(linktext)  // 仅取 path，丢弃 subpath 与显示名

// 解析到唯一目标文件（关键：必须传入 sourcePath）
getFirstLinkpathDest(linkpath, sourcePath) → TFile | null

// 解析子路径（标题 / 块）
resolveSubpath(cache, subpath) → HeadingSubpathResult | BlockSubpathResult | null
```

**Aliases**：frontmatter `aliases` 注册后，`[[别名]]` 可 resolve 到该文件；需在启动时构建 `alias → path` 索引（见 §4.4）。

**未解析链接**：目标文件尚不存在时进入 `unresolvedLinks`；图谱可显示虚节点；创建同名文件后自动 resolve。

#### 3.4.4 重命名与链接维护

**Settings → Files and links → Automatically update internal links**（默认开启）：

- 重命名 / 移动 `.md` 时，Obsidian 扫描 vault 内 wikilink 与 Markdown 内部链接并**批量改写**。
- 关闭后改为提示确认。
- 链接维护作用域为**当前 vault**；vault 嵌套 vault 时链接可能无法正确更新（官方不建议嵌套）。

#### 3.4.5 反链（Backlinks）

反链是内部链接的**反向视图**：「哪些笔记链接到了当前笔记」。

**产品层（Obsidian UI）**

- 打开笔记后，右侧 **Backlinks** 面板列出所有入站引用。
- 每条展示：来源文件、链接上下文片段、未解析链接（Linked mentions）等。
- **Outgoing links** 面板展示当前笔记的出站链接（与 `getFileCache(file).links` 对应）。

**实现层（无独立反链表）**

Obsidian **不单独存储** backlinks 表，而是由正向索引派生：

```typescript
// 正向：出站（建图谱边的直接来源）
resolvedLinks: Record<sourcePath, Record<destPath, number>>

// 未解析出站
unresolvedLinks: Record<sourcePath, Record<unresolvedKey, number>>

// 反向：入站（派生）
function buildBacklinks(resolved: typeof resolvedLinks): Record<destPath, Set<sourcePath>> {
  const backlinks = {};
  for (const [src, dests] of Object.entries(resolved)) {
    for (const dest of Object.keys(dests)) {
      (backlinks[dest] ??= new Set()).add(src);
    }
  }
  return backlinks;
}
```

插件常用 API：

| API | 用途 | 备注 |
|-----|------|------|
| `getFileCache(file)?.links` | 当前文件出站链接列表 | 官方 |
| `resolvedLinks` | 全局正向邻接表 | 官方 |
| `getFirstLinkpathDest(path, sourcePath)` | 单条链接 resolve | 官方 |
| `getBacklinksForFile(file)` | 单文件全部反链 | 非官方，大 vault 上较慢 |

**与图谱的关系**

| 方向 | 图谱表现 |
|------|----------|
| 出站 | 当前节点 → 目标的连线 |
| 入站 | 其他节点 → 当前节点的连线；**入链数**影响节点大小（被引用越多，圆越大） |

#### 3.4.6 MetadataCache 链接相关字段

单文件解析结果 `CachedMetadata` 中与链接相关的主要字段：

| 字段 | 类型 | 含义 |
|------|------|------|
| `links` | `LinkCache[]` | 正文 wikilink / 内部 Markdown 链接 |
| `embeds` | `EmbedCache[]` | `![[...]]` 嵌入 |
| `frontmatterLinks` | `FrontmatterLinkCache[]` | frontmatter 内 `[[...]]` |
| `tags` | `TagCache[]` | `#tag`（非链接，但可进图谱） |
| `blocks` | `Record<string, BlockCache>` | 块 ID，供 `[[#^id]]` 反查 |
| `headings` | `HeadingCache[]` | 标题，供 `[[#Heading]]` 反查 |

全局索引：

| 字段 | 含义 |
|------|------|
| `resolvedLinks` | 已 resolve 的 source → dest 及次数 |
| `unresolvedLinks` | 未 resolve 的 source → 链接文本及次数 |

事件：`resolved` 在链接图构建完成时触发；单文件变更后增量更新对应条目。

#### 3.4.7 内部链接 → 图谱边（小结）

```
用户书写 [[B]] 或 [t](B.md) 或 frontmatter "[[B]]"
        ↓
Parse → links / frontmatterLinks / embeds
        ↓
getFirstLinkpathDest(path, sourcePath)  [+ aliases 回退]
        ↓
resolvedLinks[source][dest] += 1
        ↓
反查得 backlinks[dest] ∋ source
        ↓
Graph View 读取 resolvedLinks 画边；节点大小 ∝ 入度
```

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

## 10. The One 记忆落到 Obsidian 后的关联设计

### 10.1 结论：可以基于图谱找到关联，但不能只依赖 Obsidian 图谱

将 The One 的长期记忆存成 Obsidian `.md` 后，**可以**通过 Obsidian 关系图谱找到对应记忆的关联，但这个能力本质上只覆盖「显式内部链接」层：

```
memory note A 中写了 [[memory note B]]
        ↓
Obsidian 解析为 resolvedLinks[A][B]
        ↓
Graph View / Local Graph / Backlinks 能看到 A ↔ B 的连接
```

它能解决：

- 从某条记忆进入 Local Graph，查看一跳/多跳关联记忆。
- 通过 Backlinks 找到“哪些记忆引用了当前记忆”。
- 通过 tags / folders / Bases 过滤出同类记忆。
- 通过 `.canvas` 手工组织一组关键记忆的可视化关系。

它不能原生解决：

- 区分 `supports` / `contradicts` / `supersedes` / `derived_from` 等**关系语义**。
- 表达关系权重、方向策略、冲突惩罚、过时惩罚。
- 按 `scope/state/tier/confidence/importance` 做严格在线检索过滤。
- 保证大规模检索性能与可解释排序。

因此 The One 不应把 Obsidian Graph 当成唯一关系数据库，而应采用：

```
Obsidian 内部链接 = 人可见、可编辑、可图谱化的显式关联
The One relation index = 机器可检索、可排序、可治理的强语义关系
```

### 10.2 设计原则

1. **文件是长期记忆真相源**：每条稳定或待审记忆对应一个 `.md` 文件。
2. **链接是 Obsidian 图谱入口**：需要出现在图谱中的关系必须落成 `[[...]]`，不能只存在 JSON 字段里。
3. **关系语义必须结构化**：typed relation 放在 frontmatter，The One 解析后生成 `memory_relation` 派生索引。
4. **正文保留人类语境**：关系为什么存在，应在正文的 `Related Memories` / `Evidence` 段落中解释。
5. **检索不直接依赖 UI Graph**：在线召回仍走 The One 自建 FTS/vector/metadata/relation expansion。
6. **Obsidian 可编辑，索引可重建**：用户手工改 `.md` 后，The One watcher 重新解析并更新索引。

### 10.3 Memory Note 作为图谱节点

每条记忆建议使用稳定文件名，避免标题变动破坏系统主键：

```
memories/
  decisions/
    mem_489a0f2e-obsidian-memory-projection.md
  constraints/
    mem_a13c9d20-no-system-prompt-injection.md
  failures/
    mem_b72e1130-sqlite-fts-fallback.md
  evidence/
    ev_20260609-obsidian-research.md
```

文件名设计：

| 部分 | 说明 |
|------|------|
| `mem_489a0f2e` | 稳定短 ID，来自 The One `memory_item.id` |
| `obsidian-memory-projection` | 可读 slug，可随标题变化但不作为主键 |
| 目录 | 辅助 Obsidian 浏览，不作为唯一 scope 判定 |

frontmatter 中必须保留完整 ID：

```yaml
---
theone_id: mem_489a0f2e61af9179c892c12394bd8e40
node_type: memory
memory_type: decision
scope: project_local
workspace_id: ws_theone
project_id: the-one
repo_id: the-one
state: pending_review
tier: long_term
confidence: 0.82
importance: 0.88
created_at: 2026-06-09T10:30:00
updated_at: 2026-06-09T10:30:00
tags:
  - theone/memory
  - theone/decision
  - project/the-one
aliases:
  - Obsidian memory projection
---
```

### 10.4 关系表达：typed frontmatter + 正文 wikilink 双通道

推荐每条强关系在 frontmatter 中用**关系类型字段**表达：

```yaml
---
supports:
  - "[[mem_a13c9d20-no-system-prompt-injection]]"
contradicts:
  - "[[mem_b72e1130-sqlite-only-storage]]"
supersedes:
  - "[[mem_c92ad001-old-vault-design]]"
related:
  - "[[mem_d41f2087-doc-snapshot-model]]"
evidence:
  - "[[ev_20260609-obsidian-research]]"
---
```

同时在正文中保留可读关系说明：

```markdown
## Related Memories

- supports [[mem_a13c9d20-no-system-prompt-injection]]：Obsidian 只作为人可读记忆层，避免把记忆提升为 system prompt。
- contradicts [[mem_b72e1130-sqlite-only-storage]]：纯 SQLite 方案缺少用户可维护知识图谱。
- supersedes [[mem_c92ad001-old-vault-design]]：新方案改为 Obsidian 真相源 + SQLite 派生索引。

## Evidence

- [[ev_20260609-obsidian-research]]：调研 Obsidian 存储、索引、图谱与 Bases。
```

这种“双通道”的意义：

| 通道 | 消费方 | 用途 |
|------|--------|------|
| frontmatter typed links | The One parser / Bases / 插件 | 生成 `memory_relation`，保留关系类型 |
| 正文 wikilink | Obsidian Graph / Backlinks / 人类阅读 | 进入图谱并解释关系原因 |

注意：如果只在 frontmatter 中写链接，Obsidian 已能识别为 `frontmatterLinks`，但用户打开 Graph 时看不到关系语境；如果只在正文中写链接，The One 需要从自然语言推断关系类型，不稳定。因此两者都保留。

### 10.5 关系类型设计

The One 当前在线检索已使用强关系集合：

| 关系 | 方向语义 | 检索影响 | Obsidian 表达 |
|------|----------|----------|----------------|
| `supports` | A 支持 B | 提升 relation support | `supports: ["[[B]]"]` + 正文说明 |
| `contradicts` | A 与 B 冲突 | 产生 conflict penalty，提示冲突 | `contradicts: ["[[B]]"]` |
| `supersedes` | A 取代 B | B 过时，A 优先 | `supersedes: ["[[B]]"]` |
| `superseded_by` | A 被 B 取代 | A 降权或过滤 | 可由 B 的 `supersedes` 反向派生 |
| `derived_from` | A 来源于 evidence/doc/event | 用于可解释性 | `evidence: ["[[E]]"]` |
| `related` | 弱相关 | 仅用于图谱探索，不默认强排序 | `related: ["[[B]]"]` |

工程建议：`superseded_by` 不必双写，优先从 `supersedes` 反向派生，避免双向字段不一致。

### 10.6 基于 Obsidian 图谱找关联的三种方式

#### 方式 A：Obsidian UI 图谱查找

适合人工探索：

1. 打开某条 `mem_*.md`。
2. 使用 Local Graph。
3. depth=1 查看直接关系；depth=2 查看二跳上下文。
4. Backlinks 面板查看入站引用。
5. Graph filter 限制 `path:memories/` 或 `tag:#theone/memory`。

优点：零开发、直观。
限制：拿不到稳定 typed relation、weight、排序分数；不适合作为 Agent 在线检索依据。

#### 方式 B：Obsidian Bases / Search 查询

适合在 vault 内建立可视化视图。Bases 能读取 `file.links`、`file.backlinks`、`file.tags`、frontmatter note properties，并支持 `file.hasLink(...)`。

示例用途：

- 待审核记忆：`state == "pending_review"`。
- 冲突记忆：`contradicts` 非空。
- 与当前笔记相关：`file.hasLink(this.file)`。
- 项目内决策：`memory_type == "decision" && project_id == "the-one"`。

限制：`file.backlinks` 在官方说明中属于较重操作，且变更后不总是自动刷新；The One 后端不应依赖 Bases 作为主查询引擎。

#### 方式 C：The One 自建 Obsidian Graph Index

推荐作为工程实现：

```
Vault watcher
  -> parse changed mem_*.md
  -> extract frontmatter typed links
  -> resolve [[...]] to theone_id/path
  -> upsert memory_item projection
  -> upsert memory_relation
  -> rebuild memory_item_fts / embedding / doc_snapshot
```

查询某条记忆的关联：

```sql
-- 出站强关系
select relation_type, target_id, weight
from memory_relation
where source_id = :memory_id;

-- 入站强关系
select relation_type, source_id, weight
from memory_relation
where target_id = :memory_id;
```

查询图谱候选：

```
1. 以 memory_id 找 path
2. 从 parsed graph 取 outgoing / incoming links
3. 按 relation_type 分层
4. 过滤 state/tier/scope
5. 按 relation weight + confidence + importance + recency 排序
```

### 10.7 推荐索引表/派生结构

Obsidian 文件是真相源，但 The One 应维护派生索引：

```text
obsidian_note
  note_id / theone_id / path / content_hash / mtime / node_type

obsidian_link
  source_path / target_path / source_id / target_id
  link_kind: body | frontmatter | embed | canvas
  raw_link / line / col / resolved

memory_relation
  source_id / target_id / relation_type / weight
  derived_from: obsidian_link | rule_based | manual

memory_item_fts
  memory_id / search_text
```

其中 `obsidian_link` 是通用链接图，`memory_relation` 是业务强关系图。二者不要混成一张表：

| 图 | 语义 | 数据来源 | 用途 |
|----|------|----------|------|
| `obsidian_link` | “A 链接到 B” | Markdown/Canvas 解析 | 图谱浏览、反链、弱关联 |
| `memory_relation` | “A 以某种业务语义关联 B” | typed frontmatter/rules/manual | 检索扩展、冲突处理、取代关系 |

### 10.8 关联发现策略

从一条记忆 `M` 找关联，建议分四层：

1. **强显式关系**：frontmatter typed links → `memory_relation`。
2. **图谱邻居**：正文/属性/canvas 的 `[[...]]` → `obsidian_link`。
3. **标签与实体共现**：同 `tags/entities/project_id/memory_type` 的记忆。
4. **文本/向量近邻**：FTS/vector 召回后再用关系图 rerank。

在线注入上下文时只默认使用第 1 层和高置信第 2 层；第 3/4 层作为候选扩展，必须受 token budget、scope 和 state 控制。

### 10.9 边界与风险

| 风险 | 原因 | 处理 |
|------|------|------|
| 只靠 Graph 看到“有关”，不知道“为什么有关” | Obsidian 边没有关系类型 | typed frontmatter + 正文说明 |
| 用户手工改名导致链接漂移 | 文件名/路径变化 | `theone_id` 作为主键，path 作为可变属性 |
| 同名 note resolve 错误 | shortest path 存在歧义 | 文件名带短 ID；写链接时优先使用带 ID slug |
| frontmatter 与正文关系不一致 | 双通道维护成本 | parser 以 frontmatter 为准，正文缺失时自动补全或诊断 |
| 图谱过密 | 每条记忆都互链 | 只写高价值强关系；弱共现放 tags/entities，不全部写 wikilink |
| 删除/归档不可见 | Obsidian 删除是文件操作 | tombstone/状态字段仍由 The One 治理 |

### 10.10 推荐落地路径

1. **Phase 1：只读投影**
   - 从 SQLite 导出 `memory_item` 为 `.md`。
   - `memory_relation` 写成 frontmatter typed links + 正文 `Related Memories`。
   - Obsidian 只用于人工浏览和 Local Graph 验证。

2. **Phase 2：双向解析**
   - The One watcher 监听 `mem_*.md`。
   - 解析 frontmatter 反写派生索引。
   - 增加诊断：frontmatter relation 与正文 wikilink 不一致。

3. **Phase 3：Obsidian 作为长期记忆真相源**
   - `memory.remember` 直接写 Markdown。
   - SQLite 变为可重建索引与生命周期账本。
   - `memory.search/context` 继续使用 FTS + relation expansion，不直接依赖 Obsidian UI。

---

## 11. 验收清单（独立实现）

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
- [ ] Wikilink 与 Markdown 内部链接均进入 resolvedLinks
- [ ] frontmatter `"[[link]]"` 计入 frontmatterLinks
- [ ] 代码块内 wikilink 不被解析
- [ ] 三种 New link format 模式下 resolve 结果符合预期
- [ ] 反链与 resolvedLinks 反向遍历结果一致
- [ ] Obsidian URI / 外链不产生内部图谱边

---

## 12. obsidianmd GitHub 生态与工程实现调研

> 调研入口：[obsidianmd/obsidian-releases](https://github.com/obsidianmd/obsidian-releases)
> 调研日期：2026-06-08

### 12.1 重要前提：obsidian-releases 不是源码仓库

[obsidian-releases](https://github.com/obsidianmd/obsidian-releases) README 明确声明：

> **Obsidian is not open source software** and this repo _DOES NOT_ contain the source code of Obsidian.

该仓库实际用途：

| 内容 | 文件/目录 | 说明 |
|------|-----------|------|
| 桌面端发布清单 | `desktop-releases.json` | 最新版号、asar 下载 URL、hash、签名 |
| 社区插件目录 | `community-plugins.json` | 插件名/作者/描述；客户端据此拉取插件 |
| 社区主题目录 | `community-css-themes.json` | 主题列表 |
| 插件统计/下架 | `community-plugin-stats.json` 等 | 运营数据 |

`desktop-releases.json` 示例（截至调研时）：

```json
{
  "latestVersion": "1.12.7",
  "downloadUrl": "https://github.com/obsidianmd/obsidian-releases/releases/download/v1.12.7/obsidian-1.12.7.asar.gz"
}
```

**结论**：核心存储、索引、图谱的**工程实现代码闭源**，打包在 Electron 的 `obsidian-x.x.x.asar` 内。工程细节需通过 **官方 API 类型定义**、**帮助文档**、**开放文件格式规范** 及社区逆向类型（如 obsidian-typings）交叉还原。

### 12.2 obsidianmd 组织仓库地图

| 仓库 | 与核心模型的关系 | 可获工程信息深度 |
|------|------------------|------------------|
| [obsidian-releases](https://github.com/obsidianmd/obsidian-releases) | 发布与插件目录 | 仅版本/分发，无实现 |
| [obsidian-api](https://github.com/obsidianmd/obsidian-api) | **插件 API 类型定义**（`obsidian.d.ts`） | 高：Vault / MetadataCache / CachedMetadata 契约 |
| [obsidian-help](https://github.com/obsidianmd/obsidian-help) | 用户与架构说明文档 | 中：存储模型、图谱行为 |
| [jsoncanvas](https://github.com/obsidianmd/jsoncanvas) | **`.canvas` 开放格式规范** | 高：Canvas 节点/边 schema |
| [obsidian-sample-plugin](https://github.com/obsidianmd/obsidian-sample-plugin) | 插件开发模板 | 低：展示如何**调用** API |
| [obsidian-clipper](https://github.com/obsidianmd/obsidian-clipper) | 浏览器剪藏扩展 | 无关核心存储 |

### 12.3 应用架构：Electron + 双层存储

```
┌─────────────────────────────────────────────────────────────┐
│  Obsidian App (Electron, obsidian-x.x.x.asar, 闭源)         │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────────────┐ │
│  │ App         │  │ Workspace    │  │ Core Plugins        │ │
│  │ vault       │  │ UI 布局      │  │ Graph / Search / …  │ │
│  │ metadataCache│ │              │  │                     │ │
│  └──────┬──────┘  └──────────────┘  └──────────┬──────────┘ │
│         │                                      │            │
│  ┌──────▼──────────────────────────────────────▼──────────┐ │
│  │ MetadataCache（内存索引 + Web Worker 解析 + IndexedDB）  │ │
│  └──────┬─────────────────────────────────────────────────┘ │
│         │ watchVaultChanges / file events                   │
└─────────┼───────────────────────────────────────────────────┘
          │ DataAdapter (fs / mobile adapter)
┌─────────▼───────────────────────────────────────────────────┐
│  Vault 目录（用户磁盘，真相源）                               │
│  *.md  *.canvas  附件  .obsidian/配置                        │
└─────────────────────────────────────────────────────────────┘
```

**设计原则**（官方帮助文档）：

- **Local-first**：笔记为本地纯文本，默认无网络请求
- **文件即真相源**：Metadata Cache 可从文件重建
- **索引即派生**：图谱、反链、搜索均依赖缓存，不另建用户不可见的专有笔记格式

### 12.4 核心存储模型

#### 层 1：Vault 文件系统（持久真相源）

`Vault`（`obsidian.d.ts`）通过 `DataAdapter` 读写本地文件：

| 概念 | API / 路径 | 说明 |
|------|------------|------|
| Vault 根 | `vault.getRoot()` | 用户选择的文件夹 |
| 配置目录 | `vault.configDir` | 默认 `.obsidian/` |
| 文件对象 | `TFile` | `path`（vault 绝对路径）、`basename`、`extension`、`stat` |
| 读文本 | `vault.read` / `vault.cachedRead` | 后者走文件内容缓存 |
| 监听变更 | `vault.on('create'/'modify'/'delete'/'rename')` | 驱动 MetadataCache 增量更新 |

`.obsidian/` 关键文件（库级）：

| 文件 | 用途 |
|------|------|
| `app.json` | 链接格式、排除规则、编辑器设置 |
| `graph.json` | Graph View 筛选器与力导向参数 |
| `workspace.json` | 工作区布局（常 gitignore） |
| `plugins/` | 社区插件 `main.js` + `manifest.json` |

#### 层 2：运行时索引（派生，IndexedDB 持久化）

官方文档：Metadata Cache 存于 **IndexedDB**，用于在应用关闭后避免全量重扫。

社区逆向（[obsidian-typings](https://github.com/Fevol/obsidian-typings)）揭示的内部结构：

| 内部字段 | 推测用途 |
|----------|----------|
| `metadataCache` | `文件 hash → CachedMetadata` |
| `fileCache` | `path → FileCacheEntry`（文件内容与 hash） |
| `resolvedLinks` / `unresolvedLinks` | 全局链接图（官方 API 公开） |
| `uniqueFileLookup` | `basename → TFile[]`（同名消歧） |
| `worker` | Web Worker 异步解析 Markdown |
| `workQueue` / `linkResolverQueue` | 解析与 resolve 任务队列 |
| `db: IDBDatabase` | IndexedDB 连接 |
| `saveMetaCache` / `saveFileCache` | 解析结果落库 |

**生命周期事件**（官方 `MetadataCache.on`）：

| 事件 | 触发时机 |
|------|----------|
| `changed` | 单文件索引完成，`CachedMetadata` 可用 |
| `resolve` | 单文件链接 resolve 完成 |
| `resolved` | 全库链接 resolve 完成（每次批量变更后） |
| `deleted` | 文件删除，附带 best-effort 旧 cache |

注意：`changed` **不在 rename 时触发**（性能考量），需监听 `vault.on('rename')` 并调用相关更新（[#77](https://github.com/obsidianmd/obsidian-api/issues/77)）。

### 12.5 开放文件格式规范

#### Markdown（`.md`）

- 无独立 schema 仓库；格式为 **CommonMark + Obsidian 扩展**（wikilink、embed、块 ID、Properties）
- Properties 底层为 **YAML frontmatter**（或 JSON frontmatter，读入后转 YAML）

#### JSON Canvas（`.canvas`）

开源规范：[obsidianmd/jsoncanvas](https://github.com/obsidianmd/jsoncanvas) spec/1.0.md

```json
{
  "nodes": [{ "id": "n1", "type": "file", "file": "note.md", "x": 0, "y": 0, "width": 400, "height": 300 }],
  "edges": [{ "id": "e1", "fromNode": "n1", "toNode": "n2" }]
}
```

节点类型：`text` | `file` | `link` | `group`。`file` 节点可带 `subpath: "#Heading"`。

#### 附件

二进制原样存储；通过 `![[file.png]]` 或 Canvas `file` 节点引用。

### 12.6 索引流水线（工程还原）

结合官方 API 与 obsidian-typings，索引流程可还原为：

```
vault 文件事件 (create/modify/delete/rename)
        ↓
watchVaultChanges() → 过滤 userIgnoreFilters（Settings 排除规则）
        ↓
computeFileMetadataAsync(file)
        ↓
worker: 解析 Markdown → CachedMetadata
        │   ├─ links[]      (ReferenceCache: link, original, displayText, position)
        │   ├─ embeds[]
        │   ├─ tags[]
        │   ├─ headings[] / blocks{} / sections[]
        │   └─ frontmatter + frontmatterLinks[]
        ↓
saveMetaCache(hash, entry) → IndexedDB
        ↓
on('changed', file, data, cache)
        ↓
linkResolver / resolveLinks(path)
        │   对每个 link: getFirstLinkpathDest(getLinkpath(link), sourcePath)
        │   结合 uniqueFileLookup + aliases
        ↓
更新 resolvedLinks[source][dest]++ 或 unresolvedLinks[source][key]++
        ↓
on('resolve', file) → on('resolved') 全库完成
```

**CachedMetadata 官方字段**（`obsidian.d.ts`）：

```typescript
interface CachedMetadata {
  links?: LinkCache[];           // extends ReferenceCache
  embeds?: EmbedCache[];
  tags?: TagCache[];
  headings?: HeadingCache[];
  blocks?: Record<string, BlockCache>;
  frontmatter?: FrontMatterCache;
  frontmatterLinks?: FrontmatterLinkCache[];
  sections?: SectionCache[];
  listItems?: ListItemCache[];
  // footnotes, footnoteRefs, referenceLinks ...
}

interface Reference {
  link: string;        // 链接目标文本
  original: string;    // 文档中的原文
  displayText?: string; // [[path|display]] 的 display 部分
}
```

### 12.7 数据关联模型

Obsidian 不在 vault 内维护单独的「关系数据库文件」，关联由三层构成：

| 层级 | 机制 | 存储位置 |
|------|------|----------|
| **显式链接** | wikilink / md link / frontmatter link | 写在 `.md` 正文 → 解析进 `links` / `frontmatterLinks` |
| **解析图** | `resolvedLinks` / `unresolvedLinks` | 内存 + IndexedDB（派生） |
| **语义关联** | 同 tag、搜索、Bases 查询 | tag 索引 / 搜索索引（闭源实现） |

**关联查询官方入口**：

```typescript
// 出站
const cache = app.metadataCache.getFileCache(file);
cache?.links;  // LinkCache[]

// 全局正向
app.metadataCache.resolvedLinks;

// 单条 resolve
app.metadataCache.getFirstLinkpathDest(linkpath, sourcePath);

// 反链（非官方，大库慎用）
app.metadataCache.getBacklinksForFile(file);  // obsidian-typings
```

**Canvas 关联**：`.canvas` 的 `edges` 是**白板内节点间**连线；`type:file` 节点通过 `file` 字段关联 vault 内文件，进入图谱需额外解析（社区工具 obsidian-parse 已覆盖）。

### 12.8 关系图谱（Graph View）工程实现

Graph View 为**核心插件**（闭源），不单独存储图数据，运行时从 MetadataCache 构建：

```
metadataCache.resolvedLinks
        + 可选 tags / attachments / unresolved
        + graph.json 过滤器（search / colorGroups / showOrphans …）
        ↓
构建 nodes[] + links[]
        ↓
力导向仿真（早期 D3.js → 现 PixiJS WebGL 渲染 + 自研/内嵌力模拟）
        ↓
Global Graph 或 Local Graph（BFS depth）
```

**`.obsidian/graph.json`** 持久化 UI 状态（非图数据本身）：

| 字段类 | 示例 | 作用 |
|--------|------|------|
| 筛选 | `search`, `showTags`, `hideUnresolved`, `showOrphans` | 决定渲染哪些节点/边 |
| 外观 | `showArrow`, `nodeSizeMultiplier`, `lineSizeMultiplier` | 节点与连线样式 |
| 力参数 | `centerStrength`, `repelStrength`, `linkStrength`, `linkDistance` | 布局物理参数 |

**节点大小**：官方文档——指向某节点的引用（入链）越多，圆越大；实现上即 `resolvedLinks` 反向计数的可视化。

### 12.9 闭源边界与调研方法

| 可查 | 不可查（闭源 asar 内） |
|------|------------------------|
| API 契约（obsidian-api） | Markdown 解析器具体实现 |
| 存储/help 文档 | 全文搜索索引结构 |
| JSON Canvas 规范 | Graph View 渲染与力模拟源码 |
| 社区逆向类型 | IndexedDB 表 schema 细节 |
| 第三方 obsidian-parse 复现 | Sync 协议细节 |

**推荐调研路径**：

1. [obsidian-releases](https://github.com/obsidianmd/obsidian-releases) → 确认版本与分发方式
2. [obsidian-api/obsidian.d.ts](https://github.com/obsidianmd/obsidian-api/blob/master/obsidian.d.ts) → Vault / MetadataCache 契约
3. [obsidian-help](https://github.com/obsidianmd/obsidian-help) → 存储与图谱行为
4. [jsoncanvas/spec/1.0.md](https://github.com/obsidianmd/jsoncanvas/blob/main/spec/1.0.md) → Canvas 格式
5. 插件实验：`app.metadataCache.resolvedLinks`、`on('resolved')` 观察索引时序
6. 独立复现：参考 [obsidian-parse](https://github.com/agent-hanju/obsidian-parse) 从 vault 文件直接建图

### 12.10 与 The One 的工程对照

| Obsidian 工程选择 | The One 可借鉴点 |
|-------------------|------------------|
| 文件为真相源，索引可重建 | `raw_event` + 文件 artifact 双层 |
| MetadataCache 异步 Worker 解析 | ingest 异步管道，避免阻塞主路径 |
| resolvedLinks 邻接表 | 实体引用边表 / code graph 边 |
| IndexedDB 缓存派生索引 | SQLite + BM25 / 向量索引 |
| Graph 为视图层，不持久化边 | 图谱查询为 retrieval 视图，边由事件/引用派生 |
| 开放 `.canvas` 格式 | 结构化 envelope / 开放导出格式 |

---

## 参考

### obsidianmd GitHub

- [obsidian-releases](https://github.com/obsidianmd/obsidian-releases)（发布与插件目录，**非源码**）
- [obsidian-api](https://github.com/obsidianmd/obsidian-api)（`obsidian.d.ts` 插件 API）
- [obsidian-help](https://github.com/obsidianmd/obsidian-help)（官方帮助文档源）
- [jsoncanvas](https://github.com/obsidianmd/jsoncanvas)（`.canvas` 开放规范）
- [obsidian-typings](https://github.com/Fevol/obsidian-typings)（社区逆向类型，非官方）

### 存储与图谱

- [How Obsidian stores data](https://help.obsidian.md/data-storage)（[中文](https://obsidian.md/zh/help/data-storage)）
- [Graph view](https://help.obsidian.md/plugins/graph)（[中文·关系图谱](https://obsidian.md/zh/help/plugins/graph)）

### 内部链接

- [Internal links](https://help.obsidian.md/links)（[中文·内部链接](https://obsidian.md/zh/help/links)）
- [Aliases](https://help.obsidian.md/aliases)
- [Properties (YAML frontmatter)](https://help.obsidian.md/properties)

### 开发者 API

- [MetadataCache](https://docs.obsidian.md/Reference/TypeScript+API/MetadataCache)
- [MetadataCache.resolvedLinks](https://docs.obsidian.md/Reference/TypeScript+API/MetadataCache/resolvedLinks)
- [CachedMetadata](https://docs.obsidian.md/Reference/TypeScript+API/CachedMetadata)
- [parseLinktext / getFirstLinkpathDest](https://docs.obsidian.md/Reference/TypeScript+API/MetadataCache/getFirstLinkpathDest)
- [obsidian-api](https://github.com/obsidianmd/obsidian-api)

### 其他

- [JSON Canvas spec](https://jsoncanvas.org/)
- [obsidian-parse](https://github.com/agent-hanju/obsidian-parse)
