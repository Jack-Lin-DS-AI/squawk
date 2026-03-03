# Squawk - Implementation TODO

## Phase 0: 專案初始化
- [ ] `go mod init` 建立 Go module
- [ ] 建立目錄結構
- [ ] 建立 CLAUDE.md（專案規範）

## Phase 1: 核心 — 規則引擎
- [ ] 定義 Rule struct（觸發條件 + 動作）
- [ ] YAML 規則解析器
- [ ] 規則評估引擎（條件匹配邏輯）
- [ ] 內建預設規則（test-only-modification 等）

## Phase 2: 監控層 — Hook Server
- [ ] HTTP server 接收 Claude Code hook 事件
- [ ] 活動追蹤（記錄工具使用歷史）
- [ ] 模式偵測（滑動視窗、頻率分析）
- [ ] Hook 設定生成器（自動產生 settings.json hook 設定）

## Phase 3: 動作層 — 介入機制
- [ ] 阻斷動作（回傳 block decision JSON）
- [ ] 指令注入（透過 hook response 注入提示）
- [ ] 通知動作（terminal notification, macOS notification）
- [ ] 動作日誌

## Phase 4: CLI
- [ ] `squawk init` — 初始化專案設定 + 安裝 hooks
- [ ] `squawk watch` — 啟動監控 server
- [ ] `squawk rules list` — 列出規則
- [ ] `squawk rules add` — 新增規則
- [ ] `squawk rules test` — 測試規則
- [ ] `squawk log` — 查看活動日誌

## Phase 5: 社群規則
- [ ] 規則 registry 概念設計
- [ ] `squawk rules fetch` — 從社群下載規則
- [ ] `squawk rules share` — 分享規則

## 未來延伸
- [ ] LLM 輔助分析（用 Haiku 判斷複雜行為模式）
- [ ] Web dashboard
- [ ] VS Code extension
- [ ] 多 session 同時監控
