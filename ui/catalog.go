package ui

import (
	"slices"
	"sort"
	"sync"
	"time"

	myeditor "github.com/egoist/mygo/ui/editor"
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

// catalogCache 按连接缓存补全目录；零值不可用，用 newCatalogCache。
type catalogCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	now      func() time.Time
	entries  map[string]catalogEntry
	inflight map[string]bool
}

func newCatalogCache() *catalogCache {
	return &catalogCache{
		ttl:      catalogTTL,
		now:      time.Now,
		entries:  map[string]catalogEntry{},
		inflight: map[string]bool{},
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
