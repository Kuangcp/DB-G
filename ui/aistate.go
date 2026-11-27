package ui

import (
	"db-g/ai"
	"db-g/app"
)

// aiState 是 AI 助手/补全/修复/改写与执行计划的状态。
type aiState struct {
	Prefs     app.AIPrefs
	ShowKey   bool   // API Key 是否显示明文（纯瞬态）
	PrefsPath string // ai.json 的位置（空 = 不落盘）
	Warn      string

	Models        []string // /models 返回的模型列表
	ModelsLoading bool
	ModelsErr     string
	ModelsSeq     int    // 异步获取轮次；旧结果作废
	ModelsKey     string // 已请求过的 baseURL+key 指纹

	Vault        *app.Vault
	TestProvider ai.Provider // 测试注入；nil = 按 Prefs 创建
	Chat         aiChatState
	TokenScale   map[string]float64 // model → 实际/估算比例

	LintOpen   bool
	LintReport aiSQLReport
	DDL        map[string]aiDDLEntry      // 相关表 DDL 缓存
	Relations  map[string]aiRelationEntry // 相关表外键缓存

	Fix        aiFixState
	Completion aiCompletionState
	Plan       planState
	Debug      aiDebugState
	Rewrite    aiRewriteState
}
