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
	if a.tree.MetaCache == nil || a.tree.MetaCacheTried[p.ID] {
		return
	}
	if a.tree.MetaCacheTried == nil {
		a.tree.MetaCacheTried = map[string]bool{}
	}
	a.tree.MetaCacheTried[p.ID] = true
	if a.tree.MetaCacheLoading == nil {
		a.tree.MetaCacheLoading = map[string]bool{}
	}
	a.tree.MetaCacheLoading[p.ID] = true
	cache := a.tree.MetaCache
	gen := a.tree.Gen
	load := func() (app.MetaCachePayload, bool, bool) { return cache.Load(p) }
	apply := func(payload app.MetaCachePayload, hit, stale bool) {
		if gen != a.tree.Gen {
			return
		}
		delete(a.tree.MetaCacheLoading, p.ID)
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
		if _, ok := a.tree.Schemas[p.ID]; !ok {
			if a.tree.Schemas == nil {
				a.tree.Schemas = map[string][]string{}
			}
			a.tree.Schemas[p.ID] = slices.Clone(payload.Schemas)
		}
	}
	for schema, counts := range payload.Counts {
		base := objectTreeKey(p.ID, schema)
		if _, ok := a.tree.Counts[base]; ok {
			continue
		}
		if a.tree.Counts == nil {
			a.tree.Counts = map[string]map[engine.ObjectKind]int{}
		}
		c := map[engine.ObjectKind]int{}
		for kind, n := range counts {
			c[kind] = n
		}
		a.tree.Counts[base] = c
	}
	for schema, byKind := range payload.Objects {
		base := objectTreeKey(p.ID, schema)
		if _, ok := a.tree.Data[base]; ok {
			// 已经在查新数据：缓存只作为兜底，不覆盖。
			continue
		}
		if a.tree.Data == nil {
			a.tree.Data = map[string]map[engine.ObjectKind][]engine.DBObject{}
		}
		if a.tree.PageStarted == nil {
			a.tree.PageStarted = map[string]bool{}
		}
		m := map[engine.ObjectKind][]engine.DBObject{}
		for kind, objs := range byKind {
			m[kind] = slices.Clone(objs)
			groupKey := objectGroupKey(base, kind)
			a.tree.PageStarted[groupKey] = true
			if stale {
				if a.tree.GroupStale == nil {
					a.tree.GroupStale = map[string]bool{}
				}
				a.tree.GroupStale[groupKey] = true
			}
		}
		a.tree.Data[base] = m
	}
	if stale {
		if a.tree.MetaCacheStale == nil {
			a.tree.MetaCacheStale = map[string]bool{}
		}
		a.tree.MetaCacheStale[p.ID] = true
	}
}

// saveMetaCache 在 UI 线程取快照，后台写盘；缓存写入失败不影响连接。
func (a *root) saveMetaCache(p app.ConnProfile) {
	if a.tree.MetaCache == nil {
		return
	}
	payload := a.metaPayload(p)
	cache := a.tree.MetaCache
	if a.tree.MetaSaveSeq == nil {
		a.tree.MetaSaveSeq = map[string]uint64{}
	}
	a.tree.MetaSaveSeq[p.ID]++
	seq := a.tree.MetaSaveSeq[p.ID]
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
	if schemas := a.tree.Schemas[p.ID]; len(schemas) > 0 {
		payload.Schemas = slices.Clone(schemas)
	}
	for base, counts := range a.tree.Counts {
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
	for base, byKind := range a.tree.Data {
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
	for _, s := range a.tree.Schemas[connID] {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for base := range a.tree.Counts {
		if s, ok := objectSchemaOf(connID, base); ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for base := range a.tree.Data {
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
	if !a.tree.MetaCacheStale[p.ID] {
		return
	}
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return
	}
	delete(a.tree.MetaCacheStale, p.ID)
	counter, ok := sess.(engine.ObjectCounter)
	if !ok {
		return
	}
	cachedSchemas := a.schemasForConn(p.ID)
	gen := a.tree.Gen
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
		if gen != a.tree.Gen {
			return
		}
		// 默认 schema 用空串作缓存键，但不要把它写进 schema 列表，
		// 否则无 schema 的驱动会多出一个空的库节点。
		if len(schemas) > 0 && !(len(schemas) == 1 && schemas[0] == "") {
			if a.tree.Schemas == nil {
				a.tree.Schemas = map[string][]string{}
			}
			a.tree.Schemas[p.ID] = schemas
		}
		if a.tree.Counts == nil {
			a.tree.Counts = map[string]map[engine.ObjectKind]int{}
		}
		for schema, c := range counts {
			a.tree.Counts[objectTreeKey(p.ID, schema)] = c
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

// refreshConnMeta 手动/DDL 后刷新一个连接的元数据：目录、树、磁盘缓存、
// AI 的 DDL 与外键缓存都失效；保留当前 schema 选择与树的展开状态，
// 展开的分组会在下一帧按需重新加载。
func (a *root) refreshConnMeta(connID string) {
	if connID == "" {
		return
	}
	schema := a.tree.ConnSchema[connID]
	a.forgetConnMeta(connID)
	if schema != "" {
		a.tree.ConnSchema[connID] = schema
	}
	if a.session.Catalog != nil {
		a.session.Catalog.invalidate(connID)
	}
	a.forgetAIDDL(connID)
	a.forgetAIRelations(connID)
	app.LogInfo("catalog", "metadata refreshed", "conn", connID)
}

// forgetConnMeta 清掉一个连接的内存元数据与磁盘缓存（删除连接/连接地址变化）。
func (a *root) forgetConnMeta(connID string) {
	a.tree.Gen++
	if a.tree.MetaCache != nil {
		a.tree.MetaCache.Delete(connID)
	}
	delete(a.tree.MetaCacheTried, connID)
	delete(a.tree.MetaCacheLoading, connID)
	delete(a.tree.MetaCacheStale, connID)
	delete(a.tree.Schemas, connID)
	delete(a.tree.ConnSchema, connID)
	delete(a.tree.SchemaLoading, connID)
	for base := range a.tree.Data {
		if connBaseMatch(connID, base) {
			delete(a.tree.Data, base)
		}
	}
	for base := range a.tree.Counts {
		if connBaseMatch(connID, base) {
			delete(a.tree.Counts, base)
		}
	}
	for key := range a.tree.Err {
		if connBaseMatch(connID, key) {
			delete(a.tree.Err, key)
		}
	}
	for key := range a.tree.Loading {
		if connBaseMatch(connID, key) {
			delete(a.tree.Loading, key)
		}
	}
	for key := range a.tree.PageStarted {
		if connBaseMatch(connID, key) {
			delete(a.tree.PageStarted, key)
		}
	}
	for key := range a.tree.GroupStale {
		if connBaseMatch(connID, key) {
			delete(a.tree.GroupStale, key)
		}
	}
}

func connBaseMatch(connID, key string) bool {
	return key == connID || strings.HasPrefix(key, connID+"|")
}
