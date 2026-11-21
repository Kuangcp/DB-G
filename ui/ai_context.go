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
	aiDDLTTL        = 10 * time.Minute
	aiDDLMaxTables  = 4
	aiDDLTimeout    = 5 * time.Second
	aiResultSamples = 3
	aiSampleColumns = 20
	aiSampleRunes   = 120
)

// aiDDLEntry 是相关表 DDL 的内存缓存；只服务 AI 上下文，不落盘。
type aiDDLEntry struct {
	ddl string
	at  time.Time
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
				vals = append(vals, truncateRunes(cellText(row[c]), aiSampleRunes))
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

// prepareAIContext 在发送/预览前补齐缺失的 DDL，然后在主线程调用 done。
// 未开启 DDL、没有会话或没有缺失时直接 done，不产生额外延迟。
func (a *root) prepareAIContext(snap ai.ContextSnapshot, done func(ai.ContextSnapshot)) {
	if !a.aiPrefs.AllowDDL || a.aiChat.preparing {
		done(snap)
		return
	}
	connID := a.activeConnID()
	if connID == "" {
		done(snap)
		return
	}
	reqs := a.aiDDLRequests(connID, snap)
	a.attachAIDDL(&snap, connID, reqs)
	var missing []aiDDLRequest
	for _, req := range reqs {
		if _, ok := a.cachedAIDDL(req.key); !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) == 0 {
		done(snap)
		return
	}
	p, ok := a.store.Get(connID)
	if !ok {
		done(snap)
		return
	}
	sess := a.sessionFor(connID)
	if sess == nil || !sess.Caps().Has(engine.CapObjectDDL) {
		done(snap)
		return
	}
	scoped, hasScoped := sess.(engine.SchemaObjectDDLer)
	lister, hasLister := sess.(engine.ObjectDDLer)
	if !hasScoped && !hasLister {
		done(snap)
		return
	}
	load := func() map[string]string {
		ctx, cancel := context.WithTimeout(context.Background(), aiDDLTimeout)
		defer cancel()
		loaded := map[string]string{}
		for _, req := range missing {
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
			loaded[req.key] = ddl
		}
		return loaded
	}
	a.aiChat.preparing = true
	a.aiChat.prepareSeq++
	seq := a.aiChat.prepareSeq
	apply := func(loaded map[string]string) {
		if seq != a.aiChat.prepareSeq {
			return
		}
		a.aiChat.preparing = false
		if a.aiDDL == nil {
			a.aiDDL = map[string]aiDDLEntry{}
		}
		for key, ddl := range loaded {
			a.aiDDL[key] = aiDDLEntry{ddl: ddl, at: time.Now()}
		}
		a.attachAIDDL(&snap, connID, reqs)
		done(snap)
	}
	if a.win == nil {
		apply(load())
		return
	}
	win := a.win
	go func() {
		loaded := load()
		a.safeUpdate(win, func() { apply(loaded) })
	}()
}
