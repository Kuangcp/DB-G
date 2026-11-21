package ui

import (
	"context"
	"strings"
	"time"

	"db-g/ai"
	"db-g/ai/plan"
	"db-g/app"
	"db-g/engine"
)

const (
	aiDDLTTL            = 10 * time.Minute
	aiDDLMaxTables      = 4
	aiDDLTimeout        = 5 * time.Second
	aiHydrateTimeout    = 8 * time.Second
	aiHydrateTables     = 8
	aiHydrateColumnsMax = 80
	aiResultSamples     = 3
	aiSampleColumns     = 20
	aiSampleRunes       = 120
)

// aiDDLEntry 是相关表 DDL 的内存缓存；只服务 AI 上下文，不落盘。
type aiDDLEntry struct {
	ddl string
	at  time.Time
}

// aiRelationEntry 是某张表外键的内存缓存（只服务 AI 检索）。
type aiRelationEntry struct {
	refs []ai.TableRef
	at   time.Time
}

// aiRelationRequest 是一次需要补齐外键的表。
type aiRelationRequest struct {
	key    string
	schema string
	table  string
}

// aiDDLRequest 是一次上下文需要补齐的 DDL 目标。
type aiDDLRequest struct {
	key    string
	table  string
	schema string
	kind   engine.ObjectKind
}

// aiResultContext 把当前结果集压成“形状”（列/行数/耗时），
// 只有用户开启时才附带少量样本行。
func (a *root) aiResultContext() *ai.ResultContext {
	rt := a.currentRuntime()
	if rt == nil {
		return nil
	}
	res := rt.Result()
	if res == nil || !res.IsQuery {
		return nil
	}
	out := &ai.ResultContext{
		SQL:         a.currentRunSQL(),
		RowCount:    len(res.Rows),
		Truncated:   res.Truncated,
		HasMore:     res.HasMore,
		ExecMillis:  res.ExecDuration.Milliseconds(),
		FetchMillis: res.FetchDuration.Milliseconds(),
	}
	for i, name := range res.Columns {
		typ := ""
		if i < len(res.ColumnTypes) {
			typ = res.ColumnTypes[i]
		}
		out.Columns = append(out.Columns, ai.ResultColumn{Name: name, Type: typ})
	}
	if a.aiPrefs.AllowSampleRows {
		rows := min(len(res.Rows), aiResultSamples)
		cols := min(len(res.Columns), aiSampleColumns)
		for _, row := range res.Rows[:rows] {
			vals := make([]string, 0, cols)
			for c := 0; c < cols; c++ {
				vals = append(vals, ai.Redact(truncateRunes(cellText(row[c]), aiSampleRunes)))
			}
			out.Sample = append(out.Sample, vals)
		}
	}
	return out
}

// aiPlanContext 把当前打开的计划树带上；没打开计划时不发送。
func (a *root) aiPlanContext() *ai.PlanContext {
	if !a.plan.open || len(a.plan.nodes) == 0 {
		return nil
	}
	return &ai.PlanContext{
		SQL: a.plan.sql, Analyze: a.plan.analyze,
		Text: plan.Text(a.plan.nodes),
	}
}

// hasColumnLister / hasSchemaColumnLister 避免在多个分支重复类型断言。
func hasColumnLister(sess engine.Session) bool {
	_, ok := sess.(engine.ColumnLister)
	return ok
}

func hasSchemaColumnLister(sess engine.Session) bool {
	_, ok := sess.(engine.SchemaColumnLister)
	return ok
}

// aiDDLKey 是缓存键：连接 + schema + 表名（大小写不敏感）。
func aiDDLKey(connID, schema, table string) string {
	return connID + "\x00" + schema + "\x00" + strings.ToLower(table)
}

// cachedAIDDL 取未过期的 DDL。
func (a *root) cachedAIDDL(key string) (string, bool) {
	e, ok := a.aiDDL[key]
	if !ok || time.Since(e.at) > aiDDLTTL || e.ddl == "" {
		return "", false
	}
	return e.ddl, true
}

// forgetAIDDL 丢弃某连接的 DDL 缓存；删除连接时调用。
func (a *root) forgetAIDDL(connID string) {
	for key := range a.aiDDL {
		if strings.HasPrefix(key, connID+"\x00") {
			delete(a.aiDDL, key)
		}
	}
}

// aiRelationKey 是外键缓存键。
func aiRelationKey(connID, schema, table string) string {
	return connID + "\x00" + schema + "\x00" + strings.ToLower(table)
}

// cachedAIRelations 取未过期的外键；ok 表示已缓存（即使 refs 为空）。
func (a *root) cachedAIRelations(key string) ([]ai.TableRef, bool) {
	e, ok := a.aiRelations[key]
	if !ok || time.Since(e.at) > aiDDLTTL {
		return nil, false
	}
	return e.refs, true
}

// attachCachedAIRelations 把已缓存的外键挂到快照的表上，供排序做一跳扩展。
func (a *root) attachCachedAIRelations(snap *ai.ContextSnapshot, connID string) {
	schemaDefault := a.connSchema[connID]
	for i := range snap.Tables {
		t := &snap.Tables[i]
		schema := t.Schema
		if schema == "" {
			schema = schemaDefault
		}
		if refs, ok := a.cachedAIRelations(aiRelationKey(connID, schema, t.Name)); ok {
			t.References = refs
		}
	}
}

// aiRelationRequests 为词法上最相关的几张表补齐外键（最多 6 张）。
func (a *root) aiRelationRequests(question string, snap ai.ContextSnapshot, connID string) []aiRelationRequest {
	candidates := ai.RankTables(question, snap.Tables, snap.ActiveSQL, 8)
	schemaDefault := a.connSchema[connID]
	seen := map[string]bool{}
	var out []aiRelationRequest
	for _, t := range candidates {
		if t.Name == "" {
			continue
		}
		schema := t.Schema
		if schema == "" {
			schema = schemaDefault
		}
		key := aiRelationKey(connID, schema, t.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, ok := a.cachedAIRelations(key); ok {
			continue
		}
		out = append(out, aiRelationRequest{key: key, schema: schema, table: t.Name})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

// attachAIRelations 把本轮加载的外键挂到快照的表上。
func (a *root) attachAIRelations(snap *ai.ContextSnapshot, reqs []aiRelationRequest) {
	byName := map[string][]ai.TableRef{}
	for _, req := range reqs {
		if refs, ok := a.cachedAIRelations(req.key); ok {
			byName[strings.ToLower(req.table)] = refs
		}
	}
	for i := range snap.Tables {
		t := &snap.Tables[i]
		if refs, ok := byName[strings.ToLower(t.Name)]; ok {
			t.References = refs
		}
	}
}

// forgetAIRelations 丢弃某连接的外键缓存。
func (a *root) forgetAIRelations(connID string) {
	for key := range a.aiRelations {
		if strings.HasPrefix(key, connID+"\x00") {
			delete(a.aiRelations, key)
		}
	}
}

// aiDDLRequests 收集当前 SQL/结果 SQL 引用的表（最多 4 张）。
func (a *root) aiDDLRequests(connID string, snap ai.ContextSnapshot) []aiDDLRequest {
	if connID == "" {
		return nil
	}
	schema := a.connSchema[connID]
	seen := map[string]bool{}
	var out []aiDDLRequest
	known := a.knownObjects(connID)
	add := func(sql string) {
		for _, rel := range relationsIn(sql) {
			if rel.Table == "" {
				continue
			}
			relSchema := rel.Schema
			if relSchema == "" {
				relSchema = schema
			}
			key := aiDDLKey(connID, relSchema, rel.Table)
			if seen[key] {
				continue
			}
			seen[key] = true
			kind := engine.ObjectTable
			if known != nil {
				if k, ok := known[strings.ToLower(rel.Table)]; ok {
					kind = k
				}
			}
			out = append(out, aiDDLRequest{key: key, table: rel.Table, schema: relSchema, kind: kind})
			if len(out) >= aiDDLMaxTables {
				return
			}
		}
	}
	add(snap.ActiveSQL)
	if snap.Result != nil && strings.TrimSpace(snap.Result.SQL) != strings.TrimSpace(snap.ActiveSQL) {
		add(snap.Result.SQL)
	}
	return out
}

// attachAIDDL 把缓存中的 DDL 放进快照；没有缓存的表先留空。
func (a *root) attachAIDDL(snap *ai.ContextSnapshot, connID string, reqs []aiDDLRequest) {
	for _, req := range reqs {
		ddl, ok := a.cachedAIDDL(req.key)
		if !ok {
			continue
		}
		found := false
		for i := range snap.Tables {
			t := &snap.Tables[i]
			if !strings.EqualFold(t.Name, req.table) {
				continue
			}
			if t.Schema != "" && req.schema != "" && !strings.EqualFold(t.Schema, req.schema) {
				continue
			}
			t.Schema = req.schema
			t.DDL = ddl
			found = true
			break
		}
		if !found {
			snap.Tables = append(snap.Tables, ai.TableContext{
				Schema: req.schema, Name: req.table, DDL: ddl,
			})
		}
	}
}

// prepareAIContext 在发送/预览前补齐缺失的表/列、外键与 DDL，然后在主线程调用 done。
// 外键用于检索扩展；DDL 只在设置开启时加载。没有缺失时不产生额外延迟。
func (a *root) prepareAIContext(question string, snap ai.ContextSnapshot, done func(ai.ContextSnapshot)) {
	if a.aiChat.preparing {
		done(snap)
		return
	}
	connID := a.activeConnID()
	if connID == "" {
		done(snap)
		return
	}
	key, schemaDefault := a.completionScope(connID)
	// 缓存命中的部分先挂上；缺失的再异步补。
	relReqs := a.aiRelationRequests(question, snap, connID)
	a.attachAIRelations(&snap, relReqs)
	var missingRel []aiRelationRequest
	for _, req := range relReqs {
		if _, ok := a.cachedAIRelations(req.key); !ok {
			missingRel = append(missingRel, req)
		}
	}
	var ddlReqs []aiDDLRequest
	var missingDDL []aiDDLRequest
	if a.aiPrefs.AllowDDL {
		ddlReqs = a.aiDDLRequests(connID, snap)
		a.attachAIDDL(&snap, connID, ddlReqs)
		for _, req := range ddlReqs {
			if _, ok := a.cachedAIDDL(req.key); !ok {
				missingDDL = append(missingDDL, req)
			}
		}
	}
	// 表名/列缺失时也走同一轮后台补齐。
	needTables := len(snap.Tables) == 0
	needColumns := false
	for _, t := range ai.RankTables(question, snap.Tables, snap.ActiveSQL, 8) {
		if len(t.Columns) == 0 {
			needColumns = true
			break
		}
	}
	if needTables {
		// 新加载出来的表也要在同一轮补列，避免第一次发送只有表名。
		needColumns = true
	}
	if len(missingRel) == 0 && len(missingDDL) == 0 && !needTables && !needColumns {
		done(snap)
		return
	}
	p, ok := a.store.Get(connID)
	if !ok {
		done(snap)
		return
	}
	sess := a.sessionFor(connID)
	if sess == nil {
		done(snap)
		return
	}
	scoped, hasScoped := sess.(engine.SchemaObjectDDLer)
	lister, hasLister := sess.(engine.ObjectDDLer)
	if !hasScoped && !hasLister {
		missingDDL = nil
	}
	if !sess.Caps().Has(engine.CapObjectDDL) {
		missingDDL = nil
	}
	fk, hasFK := sess.(engine.ForeignKeyer)
	if !hasFK || !sess.Caps().Has(engine.CapForeignKeys) {
		missingRel = nil
	}
	if needTables && (!sess.Caps().Has(engine.CapObjects)) {
		needTables = false
	}
	if needColumns && (!sess.Caps().Has(engine.CapColumns) ||
		(!hasSchemaColumnLister(sess) && !hasColumnLister(sess))) {
		needColumns = false
	}
	if !needTables && !needColumns && len(missingRel) == 0 && len(missingDDL) == 0 {
		done(snap)
		return
	}
	type prepared struct {
		ddl          map[string]string
		refs         map[string][]ai.TableRef
		tables       []ai.TableContext
		tableNames   []string
		viewNames    []string
		tablesLoaded bool
		columns      map[string][]engine.Column
	}
	load := func() prepared {
		ctx, cancel := context.WithTimeout(context.Background(), aiHydrateTimeout)
		defer cancel()
		out := prepared{
			ddl: map[string]string{}, refs: map[string][]ai.TableRef{},
			columns: map[string][]engine.Column{},
		}
		local := snap.Tables
		if needTables {
			tables, views, err := objectsOfIn(sess, schemaDefault)
			if err != nil {
				app.LogWarn("ai", "context tables failed", "conn", p.ID, "err", err.Error())
			} else if len(tables)+len(views) > 0 {
				out.tableNames, out.viewNames, out.tablesLoaded = tables, views, true
				local = make([]ai.TableContext, 0, len(tables)+len(views))
				for _, name := range tables {
					if len(local) >= 200 {
						break
					}
					local = append(local, ai.TableContext{Name: name, Schema: schemaDefault})
				}
				for _, name := range views {
					if len(local) >= 200 {
						break
					}
					local = append(local, ai.TableContext{Name: name, Schema: schemaDefault})
				}
				out.tables = local
			}
		}
		if needColumns && len(local) > 0 {
			loaded := 0
			for _, t := range ai.RankTables(question, local, snap.ActiveSQL, aiHydrateTables) {
				if len(t.Columns) > 0 || loaded >= aiHydrateTables {
					continue
				}
				schema := t.Schema
				if schema == "" {
					schema = schemaDefault
				}
				cols, err := columnsOf(sess, schema, t.Name)
				if err != nil {
					app.LogWarn("ai", "context columns failed", "conn", p.ID, "table", t.Name, "err", err.Error())
					continue
				}
				out.columns[strings.ToLower(t.Name)] = cols
				loaded++
			}
		}
		for _, req := range missingDDL {
			var ddl string
			var err error
			if hasScoped {
				ddl, err = scoped.ObjectDDLIn(ctx, req.schema, req.kind, req.table)
			} else {
				ddl, err = lister.ObjectDDL(ctx, req.kind, req.table)
			}
			if err != nil {
				app.LogWarn("ai", "context ddl failed", "conn", p.ID, "table", req.table, "err", err.Error())
				continue
			}
			out.ddl[req.key] = ddl
		}
		for _, req := range missingRel {
			rows, err := fk.ForeignKeys(ctx, req.schema, req.table)
			refs := make([]ai.TableRef, 0, len(rows))
			if err != nil {
				app.LogWarn("ai", "context fk failed", "conn", p.ID, "table", req.table, "err", err.Error())
				continue
			}
			for _, row := range rows {
				if row.RefTable == "" {
					continue
				}
				refs = append(refs, ai.TableRef{Schema: row.RefSchema, Name: row.RefTable})
			}
			out.refs[req.key] = refs
		}
		return out
	}
	a.aiChat.preparing = true
	a.aiChat.prepareSeq++
	seq := a.aiChat.prepareSeq
	apply := func(got prepared) {
		if seq != a.aiChat.prepareSeq {
			return
		}
		a.aiChat.preparing = false
		now := time.Now()
		if a.aiDDL == nil {
			a.aiDDL = map[string]aiDDLEntry{}
		}
		for key, ddl := range got.ddl {
			a.aiDDL[key] = aiDDLEntry{ddl: ddl, at: now}
		}
		if a.aiRelations == nil {
			a.aiRelations = map[string]aiRelationEntry{}
		}
		for key, refs := range got.refs {
			a.aiRelations[key] = aiRelationEntry{refs: refs, at: now}
		}
		if got.tablesLoaded {
			snap.Tables = got.tables
			if a.catalog != nil {
				a.catalog.finish(key, got.tableNames, got.viewNames, a.connKindOf(connID))
			}
		}
		for i := range snap.Tables {
			t := &snap.Tables[i]
			if cols, ok := got.columns[strings.ToLower(t.Name)]; ok {
				t.Columns = nil
				t.PrimaryKey = nil
				for i, c := range cols {
					if i >= aiHydrateColumnsMax {
						break
					}
					t.Columns = append(t.Columns, ai.ColumnContext{
						Name: c.Name, Type: c.Type, Nullable: c.Nullable, PrimaryKey: c.PrimaryKey,
					})
					if c.PrimaryKey {
						t.PrimaryKey = append(t.PrimaryKey, c.Name)
					}
				}
				if a.catalog != nil {
					a.catalog.finishColumns(key, t.Name, cols, a.connKindOf(connID))
				}
			}
		}
		a.attachAIDDL(&snap, connID, ddlReqs)
		a.attachAIRelations(&snap, relReqs)
		done(snap)
	}
	if a.win == nil {
		apply(load())
		return
	}
	win := a.win
	go func() {
		got := load()
		a.safeUpdate(win, func() { apply(got) })
	}()
}
