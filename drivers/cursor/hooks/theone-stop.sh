#!/usr/bin/env bash
# Cursor stop：每轮 Agent 回复结束触发，非会话结束。
# 记忆回合由 afterAgentResponse → turn.completed 负责；此处 intentionally no-op。
set -u
exit 0
