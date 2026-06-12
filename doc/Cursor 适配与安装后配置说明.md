# Cursor 适配与安装后配置说明

> **单一入口**：完整体验（MCP + Hooks + Rules + 验收 + 排障）请阅读  
> **[doc/cursor/README.md](./cursor/README.md)**  
> 该文档为 **全手动配置**，不依赖安装脚本。

## 快速索引

| 步骤 | 文档章节 |
|------|----------|
| 解压发布包、准备路径 | §0 准备 |
| 复制 Hook 到 `.theone-data` | §1 部署运行时文件 |
| 配置 `.cursor/mcp.json` | §2 配置 MCP |
| 配置 `.cursor/hooks.json` | §3 配置 Hooks |
| 配置 Rules | §4 配置 Rules |
| 验收与排障 | §5–§7 |

模板原文位于发布包内 `doc/cursor/`（`mcp.json`、`hooks.json`、`rule/`、`context/`）。
