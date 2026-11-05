package ui

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/engine"
)

// catalogTTL 是表/视图目录的缓存寿命；重连与「刷新补全缓存」会立即失效。
const catalogTTL = 5 * time.Minute

// catalogEntry 是一个连接的表/视图目录，以及组装好的候选（不可变，命中即复用）。
type catalogEntry struct {
	tables   []string
	views    []string
	items    []myeditor.Completion
	loadedAt time.Time
}

// columnEntry 是一个表的列候选与加载时间。
type columnEntry struct {
	items    []myeditor.Completion
	loadedAt time.Time
}

// catalogCache 按连接缓存补全目录与列；零值不可用，用 newCatalogCache。
type catalogCache struct {
	mu           sync.Mutex
	ttl          time.Duration
	now          func() time.Time
	entries      map[string]catalogEntry
	inflight     map[string]bool
	cols         map[string]map[string]columnEntry // connID → table → 列候选
	colsInflight map[string]bool                   // connID\x00table → 加载中
}

func newCatalogCache() *catalogCache {
	return &catalogCache{
		ttl:          catalogTTL,
		now:          time.Now,
		entries:      map[string]catalogEntry{},
		inflight:     map[string]bool{},
		cols:         map[string]map[string]columnEntry{},
		colsInflight: map[string]bool{},
	}
}

// get 返回该连接已组装的候选；无缓存或已过期时 ok 为 false。
func (c *catalogCache) get(connID string) ([]myeditor.Completion, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[connID]
	if !ok || c.now().Sub(e.loadedAt) >= c.ttl {
		return nil, false
	}
	return e.items, true
}

// begin 报告是否可以发起一次加载：已有新鲜缓存或正在加载时返回 false。
func (c *catalogCache) begin(connID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[connID]; ok && c.now().Sub(e.loadedAt) < c.ttl {
		return false
	}
	if c.inflight[connID] {
		return false
	}
	c.inflight[connID] = true
	return true
}

// finish 记录一次成功加载并组装候选。
func (c *catalogCache) finish(connID string, tables, views []string) {
	items := buildCatalogItems(tables, views)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, connID)
	c.entries[connID] = catalogEntry{
		tables:   slices.Clone(tables),
		views:    slices.Clone(views),
		items:    items,
		loadedAt: c.now(),
	}
}

// fail 结束一次失败的加载：不写缓存，下次可重试。
func (c *catalogCache) fail(connID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, connID)
}

// invalidate 丢弃某连接的缓存（重连、刷新、删连接）。
func (c *catalogCache) invalidate(connID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, connID)
	delete(c.inflight, connID)
	delete(c.cols, connID)
	prefix := connID + "\x00"
	for k := range c.colsInflight {
		if strings.HasPrefix(k, prefix) {
			delete(c.colsInflight, k)
		}
	}
}

// hasRelation 报告该连接的表/视图里是否有这个名字（大小写不敏感，与 SQL 一致）。
func (c *catalogCache) hasRelation(connID, name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[connID]
	if !ok {
		return false
	}
	for _, list := range [][]string{e.tables, e.views} {
		for _, n := range list {
			if strings.EqualFold(n, name) {
				return true
			}
		}
	}
	return false
}

// getColumns 返回某表的列候选；无缓存或已过期时 ok 为 false。
func (c *catalogCache) getColumns(connID, table string) ([]myeditor.Completion, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cols[connID][table]
	if !ok || c.now().Sub(e.loadedAt) >= c.ttl {
		return nil, false
	}
	return e.items, true
}

// beginColumns 报告是否可以发起一次列加载。
func (c *catalogCache) beginColumns(connID, table string) bool {
	key := connID + "\x00" + table
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.cols[connID][table]; ok && c.now().Sub(e.loadedAt) < c.ttl {
		return false
	}
	if c.colsInflight[key] {
		return false
	}
	c.colsInflight[key] = true
	return true
}

// finishColumns 记录一次成功的列加载。
func (c *catalogCache) finishColumns(connID, table string, cols []engine.Column) {
	items := buildColumnItems(cols)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.colsInflight, connID+"\x00"+table)
	if c.cols[connID] == nil {
		c.cols[connID] = map[string]columnEntry{}
	}
	c.cols[connID][table] = columnEntry{items: items, loadedAt: c.now()}
}

// failColumns 结束一次失败的列加载：不写缓存，下次可重试。
func (c *catalogCache) failColumns(connID, table string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.colsInflight, connID+"\x00"+table)
}

// buildColumnItems 组装「词表 + 该表的列」；列去重排序，Detail 给类型。
func buildColumnItems(cols []engine.Column) []myeditor.Completion {
	words := sqlWordItems()
	out := make([]myeditor.Completion, 0, len(words)+len(cols))
	out = append(out, words...)
	sorted := slices.Clone(cols)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	seen := map[string]bool{}
	for _, col := range sorted {
		if col.Name == "" || seen[col.Name] {
			continue
		}
		seen[col.Name] = true
		detail := col.Type
		if detail == "" {
			detail = "列"
		}
		out = append(out, myeditor.Completion{Label: col.Name, Detail: detail, Kind: myeditor.CompletionField})
	}
	return out
}

// buildCatalogItems 组装候选：词表在前，表（表）与视图（视图）在后；各段去重排序。
func buildCatalogItems(tables, views []string) []myeditor.Completion {
	words := sqlWordItems()
	out := make([]myeditor.Completion, 0, len(words)+len(tables)+len(views))
	out = append(out, words...)
	add := func(names []string, detail string, kind myeditor.CompletionKind) {
		sorted := slices.Clone(names)
		sort.Strings(sorted)
		seen := map[string]bool{}
		for _, n := range sorted {
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, myeditor.Completion{Label: n, Detail: detail, Kind: kind})
		}
	}
	add(tables, "表", myeditor.CompletionModule)
	add(views, "视图", myeditor.CompletionType)
	return out
}
