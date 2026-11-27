package ui

import "time"

// externalState 是外部 .sql 文件的轮询、绑定与一致性状态。
type externalState struct {
	Files       map[string]*extFile // consoleID → 磁盘一致性状态
	PollAt      time.Time
	PickOpen    bool
	PickPaths   []string
	ResolveOpen bool
	Msg         string
}
