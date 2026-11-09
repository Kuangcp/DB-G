package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"db-g/app"
	"db-g/engine"
)

// loadMetaCache 在展开数据源时异步读磁盘缓存；命中后立即铺进树，
// 过期缓存仍先用，等连接可用后后台刷新计数（分组正文展开时再刷新首页）。
func (a *root) loadMetaCache(p app.ConnProfile) {
	if a.metaCache == nil || a.metaCacheTried[p.ID] {
		return
	}
	if a.metaCacheTried == nil {
		a.metaCacheTried = map[string]bool{}
	}
	a.metaCacheTried[p.ID] = true
	if a.metaCacheLoading == nil {
		a.metaCacheLoading = map[string]bool{}
	}
	a.metaCacheLoading[p.ID] = true
	cache := a.metaCache
	gen := a.treeGen
	load := func() (app.MetaCachePayload, bool, bool) { return cache.Load(p) }
	apply := func(payload app.MetaCachePayload, hit, stale bool) {
		if gen != a.treeGen {
			return
		}
		delete(a.metaCacheLoading, p.ID)
		if !hit {
			return
		}
		a.applyMetaPayload(p, payload, stale)
	}
	if a.win == nil {
		payload, hit, stale := load()
		apply(payload, hit, stale)
		return
	}
	win := a.win
	go func() {
		payload, hit, stale := load()
		a.safeUpdate(win, func() { apply(payload, hit, stale) })
	}()
}

// applyMetaPayload 把缓存合并进当前树。只填空位，不覆盖已经查回来的新数据。
func (a *root) applyMetaPayload(p app.ConnProfile, payload app.MetaCachePayload, stale bool) {
	if len(payload.Schemas) > 0 {
		if _, ok := a.treeSchemas[p.ID]; !ok {
			if a.treeSchemas == nil {
				a.treeSchemas = map[string][]string{}
			}
			a.treeSchemas[p.ID] = slices.Clone(payload.Schemas)
		}
	}
	for schema, counts := range payload.Counts {
		base := objectTreeKey(p.ID, schema)
		if _, ok := a.treeCounts[base]; ok {
			continue
		}
		if a.treeCounts == nil {
			a.treeCounts = map[string]map[engine.ObjectKind]int{}
		}
		c := map[engine.ObjectKind]int{}
		for kind, n := range counts {
			c[kind] = n
		}
		a.treeCounts[base] = c
	}
	for schema, byKind := range payload.Objects {
		base := objectTreeKey(p.ID, schema)
		if _, ok := a.treeData[base]; ok {
			// 已经在查新数据：缓存只作为兜底，不覆盖。
			continue
		}
		if a.treeData == nil {
			a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{}
		}
		if a.treePageStarted == nil {
			a.treePageStarted = map[string]bool{}
		}
		m := map[engine.ObjectKind][]engine.DBObject{}
		for kind, objs := range byKind {
			m[kind] = slices.Clone(objs)
			groupKey := objectGroupKey(base, kind)
			a.treePageStarted[groupKey] = true
			if stale {
				if a.treeGroupStale == nil {
					a.treeGroupStale = map[string]bool{}
				}
				a.treeGroupStale[groupKey] = true
			}
		}
		a.treeData[base] = m
	}
	if stale {
		if a.metaCacheStale == nil {
			a.metaCacheStale = map[string]bool{}
		}
		a.metaCacheStale[p.ID] = true
	}
}

// saveMetaCache 在 UI 线程取快照，后台写盘；缓存写入失败不影响连接。
func (a *root) saveMetaCache(p app.ConnProfile) {
	if a.metaCache == nil {
		return
	}
	payload := a.metaPayload(p)
	cache := a.metaCache
	if a.metaSaveSeq == nil {
		a.metaSaveSeq = map[string]uint64{}
	}
	a.metaSaveSeq[p.ID]++
	seq := a.metaSaveSeq[p.ID]
	save := func() {
		if err := cache.SaveOrdered(p, payload, seq); err != nil {
			app.LogWarn("catalog", "meta cache save failed", "conn", p.ID, "err", err.Error())
		}
	}
	if a.win == nil {
		save()
		return
	}
	go save()
}

// metaPayload 深拷贝当前连接在内存里的 schema/计数/已加载对象。
func (a *root) metaPayload(p app.ConnProfile) app.MetaCachePayload {
	payload := app.MetaCachePayload{}
	if schemas := a.treeSchemas[p.ID]; len(schemas) > 0 {
		payload.Schemas = slices.Clone(schemas)
	}
	for base, counts := range a.treeCounts {
		schema, ok := objectSchemaOf(p.ID, base)
		if !ok {
			continue
		}
		if payload.Counts == nil {
			payload.Counts = map[string]map[engine.ObjectKind]int{}
		}
		c := map[engine.ObjectKind]int{}
		for kind, n := range counts {
			c[kind] = n
		}
		payload.Counts[schema] = c
	}
	for base, byKind := range a.treeData {
		schema, ok := objectSchemaOf(p.ID, base)
		if !ok {
			continue
		}
		if payload.Objects == nil {
			payload.Objects = map[string]map[engine.ObjectKind][]engine.DBObject{}
		}
		m := map[engine.ObjectKind][]engine.DBObject{}
		for kind, objs := range byKind {
			m[kind] = slices.Clone(objs)
		}
		payload.Objects[schema] = m
	}
	return payload
}

func objectSchemaOf(connID, base string) (string, bool) {
	if base == connID {
		return "", true
	}
	prefix := connID + "|"
	if strings.HasPrefix(base, prefix) {
		return strings.TrimPrefix(base, prefix), true
	}
	return "", false
}

// schemasForConn 返回当前连接在内存里的 schema 列表（含计数缓存里出现过的）。
func (a *root) schemasForConn(connID string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range a.treeSchemas[connID] {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for base := range a.treeCounts {
		if s, ok := objectSchemaOf(connID, base); ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for base := range a.treeData {
		if s, ok := objectSchemaOf(connID, base); ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// refreshStaleMeta 过期缓存的后台刷新：重取 schema 列表和每组计数；
// 已缓存的分组正文在用户展开时替换首页（treeGroupStale）。
func (a *root) refreshStaleMeta(p app.ConnProfile) {
	if !a.metaCacheStale[p.ID] {
		return
	}
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return
	}
	delete(a.metaCacheStale, p.ID)
	counter, ok := sess.(engine.ObjectCounter)
	if !ok {
		return
	}
	cachedSchemas := a.schemasForConn(p.ID)
	gen := a.treeGen
	load := func() ([]string, map[string]map[engine.ObjectKind]int) {
		schemas := cachedSchemas
		if lister, ok := sess.(engine.SchemaLister); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			fresh, err := lister.Schemas(ctx)
			cancel()
			if err == nil && len(fresh) > 0 {
				schemas = fresh
			}
		}
		if len(schemas) == 0 {
			schemas = []string{""}
		}
		counts := map[string]map[engine.ObjectKind]int{}
		for _, schema := range schemas {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			c, err := counter.ObjectCounts(ctx, schema)
			cancel()
			if err != nil {
				app.LogWarn("catalog", "meta cache refresh counts failed",
					"conn", p.ID, "schema", schema, "err", err.Error())
				continue
			}
			counts[schema] = c
		}
		return schemas, counts
	}
	done := func(schemas []string, counts map[string]map[engine.ObjectKind]int) {
		if gen != a.treeGen {
			return
		}
		// 默认 schema 用空串作缓存键，但不要把它写进 schema 列表，
		// 否则无 schema 的驱动会多出一个「▣ 」节点。
		if len(schemas) > 0 && !(len(schemas) == 1 && schemas[0] == "") {
			if a.treeSchemas == nil {
				a.treeSchemas = map[string][]string{}
			}
			a.treeSchemas[p.ID] = schemas
		}
		if a.treeCounts == nil {
			a.treeCounts = map[string]map[engine.ObjectKind]int{}
		}
		for schema, c := range counts {
			a.treeCounts[objectTreeKey(p.ID, schema)] = c
		}
		// 全部计数查询都失败时不要把旧缓存标成新鲜；下次启动/重载再试。
		if len(counts) > 0 {
			a.saveMetaCache(p)
		}
	}
	if a.win == nil {
		schemas, counts := load()
		done(schemas, counts)
		return
	}
	win := a.win
	go func() {
		schemas, counts := load()
		a.safeUpdate(win, func() { done(schemas, counts) })
	}()
}

// forgetConnMeta 清掉一个连接的内存元数据与磁盘缓存（删除连接/连接地址变化）。
func (a *root) forgetConnMeta(connID string) {
	a.treeGen++
	if a.metaCache != nil {
		a.metaCache.Delete(connID)
	}
	delete(a.metaCacheTried, connID)
	delete(a.metaCacheLoading, connID)
	delete(a.metaCacheStale, connID)
	delete(a.treeSchemas, connID)
	delete(a.connSchema, connID)
	delete(a.schemaLoading, connID)
	for base := range a.treeData {
		if connBaseMatch(connID, base) {
			delete(a.treeData, base)
		}
	}
	for base := range a.treeCounts {
		if connBaseMatch(connID, base) {
			delete(a.treeCounts, base)
		}
	}
	for key := range a.treeErr {
		if connBaseMatch(connID, key) {
			delete(a.treeErr, key)
		}
	}
	for key := range a.treeLoading {
		if connBaseMatch(connID, key) {
			delete(a.treeLoading, key)
		}
	}
	for key := range a.treePageStarted {
		if connBaseMatch(connID, key) {
			delete(a.treePageStarted, key)
		}
	}
	for key := range a.treeGroupStale {
		if connBaseMatch(connID, key) {
			delete(a.treeGroupStale, key)
		}
	}
}

func connBaseMatch(connID, key string) bool {
	return key == connID || strings.HasPrefix(key, connID+"|")
}
