package ui

import "db-g/app"

// consoleTabsState 是控制台运行态、标签/工作区视图与结果 tab 的状态。
type consoleTabsState struct {
	Consoles      map[string]*app.Console // 运行态 id → 执行状态
	ActiveConsole string
	MRU           []string              // 最近使用的标签（控制台 id 或 "vw:"+viewerID）
	ViewerTabs    map[string]*viewerTab // 表/视图临时标签（只在内存）
	ViewerOrder   []string              // 表视图标签的创建顺序
	ActiveViewer  string                // 非空 = 当前展示表视图
	ConsoleGrid   *gridState            // 控制台自己的结果网格状态

	LastVer       map[string]uint64
	LastCaret     [2]int
	StmtRuns      []stmtRun              // 当前控制台的结果 tab
	NextTabNo     int                    // 结果编号分配器
	RunsByConsole map[string]consoleRuns // consoleID → 最近一次的结果 tab
	ActiveStmt    int
	LastExecSQL   string                // 最近一次执行的语句（取更多/复制 INSERT 用）
	PageStates    map[string]*pageState // runtimeID → 分页状态
}
