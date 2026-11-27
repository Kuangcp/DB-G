package ui

import (
	"db-g/app"
	"db-g/engine"
)

// treeState 是左侧数据源/对象树及其元数据缓存的状态。
type treeState struct {
	Expand           map[string]bool
	Open             map[string]*bool
	Data             map[string]map[engine.ObjectKind][]engine.DBObject
	Counts           map[string]map[engine.ObjectKind]int
	Loading          map[string]bool
	PageStarted      map[string]bool
	Schemas          map[string][]string
	SchemaLoading    map[string]bool   // connID → schema 列表加载中
	ConnSchema       map[string]string // connID → 当前 schema/库（空 = 未选）
	Err              map[string]string
	Selected         string
	ObjSearch        string          // 搜索词；非空时过滤连接/文件夹/已加载对象
	FolderCollapsed  map[string]bool // folderID → 收起；缺省 = 展开
	MetaCache        *app.MetaCache
	MetaCacheTried   map[string]bool
	MetaCacheLoading map[string]bool
	MetaCacheStale   map[string]bool
	MetaSaveSeq      map[string]uint64
	GroupStale       map[string]bool // 过期缓存的分组；展开时替换首页
	Gen              uint64          // 重载/删除时递增，丢弃旧的异步结果
}
