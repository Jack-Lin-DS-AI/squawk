# Squawk - Research Notes

> Claude Code 行為監督工具：規則驅動的即時偵測 + 主動介入矯正

## 專案願景

監督 Claude Code 的行為，當偵測到不正常模式時（例如：反覆修改 test 讓它通過，而不去檢查 source code），主動介入並導回正軌。

### 核心功能

1. **規則引擎** — 使用者可設定觸發條件 + 觸發後動作
2. **即時監控** — 監控 Claude Code 的輸出與檔案改動
3. **主動介入** — 偵測到符合規則的異常時，中斷並注入新指令
4. **社群規則** — 收集常見 anti-patterns，讓使用者 adopt 到自己的專案

### 規則結構

每條規則包含：
- **(a) 觸發條件** — 什麼情況下觸發（例：同一 test 檔案被修改 3 次以上，但對應 source 未被修改）
- **(b) 觸發動作** — 觸發後執行什麼（例：阻斷操作 + 注入「請先檢查 source code」的指令）

---

## Claude Code Hooks 研究

### 可用 Hook 事件（18 種）

**可阻斷的事件：**
| 事件 | 說明 | 阻斷效果 |
|------|------|----------|
| PreToolUse | 工具執行前（Bash, Read, Write, Edit, Glob, Grep 等） | 取消工具呼叫 |
| PermissionRequest | 權限對話框出現時 | 拒絕/允許權限 |
| UserPromptSubmit | 使用者提交 prompt 時 | 拒絕 prompt |
| Stop | Claude 完成回應時 | 阻止 Claude 停止 |
| PreCompact | Context 壓縮前 | 阻止壓縮 |
| ConfigChange | 設定檔變更時 | 阻止變更 |
| SubagentStop | Subagent 結束時 | 阻止 subagent 停止 |
| TeammateIdle | Agent team 成員閒置時 | 阻止閒置 |
| TaskCompleted | Task 標記完成時 | 阻止完成 |

**觀察性事件：**
| 事件 | 說明 |
|------|------|
| PostToolUse | 工具執行成功後 |
| PostToolUseFailure | 工具執行失敗後 |
| SubagentStart | Subagent 啟動時 |
| SubagentStop | Subagent 結束時 |
| SessionStart | Session 開始/恢復時 |
| SessionEnd | Session 結束時 |
| Notification | Claude Code 發送通知時 |
| WorktreeCreate | Worktree 建立時 |
| WorktreeRemove | Worktree 移除時 |

### Hook 類型（4 種）

1. **command** — 執行 shell 指令，透過 stdin 接收資料，exit code 控制行為
2. **http** — POST 事件資料到 endpoint，透過 response body 回傳決策
3. **prompt** — 單輪 LLM 呼叫（預設 Haiku），用於判斷性決策
4. **agent** — 多輪 subagent 驗證，可讀檔、搜尋、執行指令（最多 50 輪）

### 阻斷機制

**Exit Code 2：**
```bash
#!/bin/bash
INPUT=$(cat)
# 分析後決定阻斷
echo "Blocked: 原因說明" >&2
exit 2  # 阻斷操作
```

**JSON Decision：**
```json
{
  "decision": "block",
  "reason": "偵測到反覆修改 test 的行為，請先檢查 source code"
}
```

**PreToolUse 專用 JSON：**
```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "請先閱讀對應的 implementation 檔案"
  }
}
```

### Hook 設定位置

- `~/.claude/settings.json` — 全域
- `.claude/settings.json` — 專案
- `.claude/settings.local.json` — 本地（gitignored）

### Hook 輸入資料結構

```json
{
  "session_id": "abc123",
  "cwd": "/path/to/project",
  "hook_event_name": "PreToolUse",
  "tool_name": "Edit",
  "tool_input": {
    "file_path": "/path/to/test_file.go",
    "old_string": "...",
    "new_string": "..."
  }
}
```

### 重要特性

- **Matcher** — 用 regex 過濾觸發的工具名稱：`"matcher": "Bash|Edit|Write"`
- **Async** — 設 `"async": true` 可背景執行不阻塞
- **Timeout** — 預設 10 分鐘，可自訂
- **環境變數** — `$CLAUDE_PROJECT_DIR`, `$CLAUDE_ENV_FILE`

---

## 競品分析

### 直接競品（Claude Code 相關）

| 專案 | GitHub | 概念 | 與 Squawk 差異 |
|------|--------|------|----------------|
| sentinel | kostandinang/sentinel | macOS 選單列監控 Claude Code sessions | 被動觀察，無主動介入 |
| claude-code-watchdog | CardScan-ai/claude-code-watchdog | GitHub Actions test failure 分析 | CI 層面，非即時 session |
| claude-code-guardian | jfpedroza/claude-code-guardian | PreToolUse hook 權限驗證 | 只做指令過濾，不分析行為模式 |
| claude-code-bash-guardian | RoaringFerrum/claude-code-bash-guardian | Bash 指令安全過濾 | 純安全層，無規則引擎 |
| overstory | jayminwest/overstory | 多 agent 協調 + watchdog | 偏向 orchestration |
| hooks-observability | disler/claude-code-hooks-multi-agent-observability | Hook 事件可視化 | 純 observability，無介入 |
| ai-observer | tobilg/ai-observer | 統一本地 AI 工具可觀測性 | Token/成本追蹤，非行為監控 |

### Squawk 差異化

**目前沒有任何專案做到「規則驅動的行為偵測 + 主動介入矯正」**

- sentinel / ai-observer = 被動監控
- guardian = 靜態指令過濾
- watchdog = CI 層面分析
- overstory = agent 協調

Squawk = **即時行為分析 + 規則引擎 + 主動介入**

---

## 命名決策

**最終選擇：squawk**

理由：
- 航空術語：飛行員對塔台的緊急通報碼
- 完美映射「偵測異常 → 發出警報 → 導正行為」
- 無同領域衝突
- 短、好記、有個性
- CLI 手感好：`squawk watch`, `squawk rules add`, `squawk init`

---

## 技術架構（初步方向）

### 語言：Go

### 核心組件

```
squawk/
├── cmd/
│   └── squawk/
│       └── main.go          # CLI 入口
├── internal/
│   ├── rules/
│   │   ├── engine.go        # 規則評估引擎
│   │   ├── rule.go          # 規則定義
│   │   └── parser.go        # YAML 規則解析
│   ├── monitor/
│   │   ├── watcher.go       # 檔案系統監控
│   │   ├── hook.go          # Hook handler (HTTP server)
│   │   └── activity.go      # 活動追蹤與模式偵測
│   ├── analyzer/
│   │   ├── analyzer.go      # 模式分析
│   │   └── llm.go           # LLM 輔助分析（可選）
│   ├── action/
│   │   ├── executor.go      # 動作執行
│   │   ├── interrupt.go     # 中斷機制
│   │   └── notify.go        # 通知
│   └── config/
│       └── config.go        # 設定管理
├── rules/
│   ├── default.yaml         # 預設規則
│   └── community/           # 社群規則
├── go.mod
├── go.sum
└── CLAUDE.md
```

### 運作流程

1. `squawk watch` 啟動 HTTP server（接收 Claude Code hooks）
2. Claude Code hooks 設定 POST 事件到 squawk server
3. squawk 接收事件 → 記錄活動 → 評估規則
4. 規則觸發 → 執行對應動作（阻斷/注入指令/通知）

### 規則範例

```yaml
rules:
  - name: test-only-modification
    description: "偵測反覆修改 test 而不檢查 source code"
    trigger:
      type: pattern
      conditions:
        - event: PostToolUse
          tool: Edit
          file_pattern: "*_test.go|*.test.ts|*.spec.ts"
          count: 3
          within: 5m
        - not:
            event: PostToolUse
            tool: Read
            file_pattern: "!*_test.go&!*.test.ts&!*.spec.ts"
    action:
      type: interrupt
      method: block_next_edit
      message: |
        偵測到你已修改 test 檔案 {count} 次但未檢查對應的 source code。
        請先閱讀相關的 implementation 檔案，確認是 test 需要修改還是 source code 有問題。
```
