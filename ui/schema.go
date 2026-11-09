package ui

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// schemaLabel 是下拉里给人看的目标名：MySQL/CK 是库名，PG 把库和 schema 都写出来。
func schemaLabel(kind, database, schema string) string {
	if schema == "" {
		return "（默认）"
	}
	if kind == "postgres" && database != "" {
		return database + "." + schema
	}
	return schema
}

// defaultSchemaFor 选默认 schema：连接里填的库优先，PG 优先 public，否则第一项。
func defaultSchemaFor(p app.ConnProfile, schemas []string) string {
	if len(schemas) == 0 {
		return ""
	}
	if p.Kind != "postgres" && p.Database != "" {
		for _, s := range schemas {
			if s == p.Database {
				return s
			}
		}
	}
	if p.Kind == "postgres" {
		for _, s := range schemas {
			if s == "public" {
				return s
			}
		}
	}
	return schemas[0]
}

// initSchemaAfterConnect 在连上后把当前 schema/库与 schema 列表装好：
// 连接里没填库（MySQL 多库账号）时自动选第一项，避免补全/元数据报「未选择数据库」。
func (a *root) initSchemaAfterConnect(p app.ConnProfile, sess engine.Session) {
	sw, ok := sess.(engine.SchemaSwitcher)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	if cur, err := sw.ActiveSchema(ctx); err == nil && cur != "" {
		a.connSchema[p.ID] = cur
	}
	lister, ok := sess.(engine.SchemaLister)
	if !ok {
		return
	}
	schemas, err := lister.Schemas(ctx)
	if err != nil || len(schemas) == 0 {
		if err != nil {
			app.LogWarn("catalog", "schemas load failed", "conn", p.ID, "err", err.Error())
		}
		return
	}
	if a.treeSchemas == nil {
		a.treeSchemas = map[string][]string{}
	}
	a.treeSchemas[p.ID] = schemas
	if a.connSchema[p.ID] == "" {
		target := defaultSchemaFor(p, schemas)
		if err := sw.UseSchema(ctx, target); err != nil {
			app.LogWarn("catalog", "default schema failed", "conn", p.ID, "schema", target, "err", err.Error())
			return
		}
		a.connSchema[p.ID] = target
	}
	a.saveMetaCache(p)
}

// ensureSchemas 惰性加载 schema/库列表；顶部下拉与对象树共用这份缓存。
func (a *root) ensureSchemas(p app.ConnProfile) {
	if len(a.treeSchemas[p.ID]) > 0 || a.schemaLoading[p.ID] {
		return
	}
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return
	}
	lister, ok := sess.(engine.SchemaLister)
	if !ok {
		return
	}
	if a.schemaLoading == nil {
		a.schemaLoading = map[string]bool{}
	}
	a.schemaLoading[p.ID] = true
	gen := a.treeGen
	load := func() ([]string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return lister.Schemas(ctx)
	}
	done := func(schemas []string, err error) {
		delete(a.schemaLoading, p.ID)
		if gen != a.treeGen {
			return
		}
		if err != nil {
			a.setTreeErr(p.ID, err.Error())
			return
		}
		if len(schemas) == 0 {
			return
		}
		if a.treeSchemas == nil {
			a.treeSchemas = map[string][]string{}
		}
		a.treeSchemas[p.ID] = schemas
		if a.connSchema[p.ID] == "" {
			a.setActiveSchema(p, defaultSchemaFor(p, schemas))
		}
		a.saveMetaCache(p)
	}
	if a.win == nil {
		schemas, err := load()
		done(schemas, err)
		return
	}
	win := a.win
	go func() {
		schemas, err := load()
		win.Update(func() { done(schemas, err) })
	}()
}

// setActiveSchema 切换当前 schema/库：更新下拉状态、失效补全缓存、展开树里的 schema 节点，
// 并把切换应用到会话（UseSchema）。有窗口时后台执行，headless 同步便于测试。
func (a *root) setActiveSchema(p app.ConnProfile, schema string) {
	if schema == "" {
		return
	}
	if a.connSchema == nil {
		a.connSchema = map[string]string{}
	}
	prev := a.connSchema[p.ID]
	a.connSchema[p.ID] = schema
	if a.catalog != nil {
		a.catalog.invalidate(p.ID)
	}
	// 展开对象树里对应的 schema 节点，用户能立刻看到该库下的对象。
	key := p.ID + "|" + schema + "|schema"
	if a.treeOpen == nil {
		a.treeOpen = map[string]*bool{}
	}
	if pr, ok := a.treeOpen[key]; ok {
		*pr = true
	} else {
		open := true
		a.treeOpen[key] = &open
	}
	if a.treeErr != nil {
		delete(a.treeErr, p.ID)
	}
	sess := a.sessionFor(p.ID)
	sw, ok := sess.(engine.SchemaSwitcher)
	if !ok {
		return
	}
	run := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		return sw.UseSchema(ctx, schema)
	}
	done := func(err error) {
		if err != nil {
			if a.connSchema[p.ID] == schema {
				a.connSchema[p.ID] = prev
			}
			a.consoleErr = "切换 schema 失败: " + err.Error()
			app.LogWarn("conn", "schema switch failed", "conn", p.ID, "schema", schema, "err", err.Error())
			return
		}
		if a.catalog != nil {
			a.catalog.invalidate(p.ID)
		}
		app.LogInfo("conn", "schema switched", "conn", p.ID, "schema", schema)
	}
	if a.win == nil {
		done(run())
		return
	}
	win := a.win
	go func() {
		err := run()
		win.Update(func() {
			done(err)
			if err == nil {
				a.retriggerCompletions()
			}
		})
	}()
}

// schemaPicker 是顶栏数据源名旁边的 schema/库下拉：只在会话支持多 schema 且能切换时显示。
func (a *root) schemaPicker(c *ui.Context) {
	id := a.activeConnID()
	if id == "" || a.store == nil {
		return
	}
	sess := a.sessionFor(id)
	if sess == nil {
		return
	}
	if _, ok := sess.(engine.SchemaLister); !ok {
		return
	}
	if _, ok := sess.(engine.SchemaSwitcher); !ok {
		return
	}
	p, ok := a.store.Get(id)
	if !ok {
		return
	}
	schemas := a.treeSchemas[id]
	if len(schemas) == 0 {
		a.ensureSchemas(p)
		if a.schemaLoading[id] {
			ui.Text(c, "加载库…").FontSize(12).TextColor(c.Theme().TextMuted)
		}
		return
	}
	t := c.Theme()
	cur := a.connSchema[id]
	shown := cur
	if shown == "" {
		shown = defaultSchemaFor(p, schemas)
	}
	sel := shown
	parts := ui.SelectBase(c, &sel)
	parts.Trigger.Gap(4).Padding(4, 8).Radius(t.Space(1.5)).Border(1, t.Border).
		Tooltip("切换当前库 / schema").Children(func() {
		ui.Text(c, schemaLabel(p.Kind, p.Database, shown)).FontSize(12).SingleLine()
		ui.Icon(c, iconChevronDown).FontSize(12).TextColor(t.TextMuted)
	})
	parts.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(t.Space(1)).Background(t.Background).Border(1, t.Border).Width(280)
		for _, s := range schemas {
			it := parts.Item(s).Padding(6, 10).Radius(t.Space(0.5))
			if it.Highlighted() {
				it.Background(t.Accent.Alpha(0.12))
			}
			it.Children(func() {
				label := ui.Text(c, schemaLabel(p.Kind, p.Database, s)).SingleLine()
				if s == shown {
					label.Bold()
				}
			})
		}
	})
	if sel != a.connSchema[id] {
		a.setActiveSchema(p, sel)
	}
}

// completionScope 返回补全缓存的键与当前 schema：不同 schema 的表/列候选不能混用。
func (a *root) completionScope(connID string) (key, schema string) {
	schema = a.connSchema[connID]
	if schema == "" {
		return connID, ""
	}
	return connID + "|" + schema, schema
}

// schemaDisplayName 返回当前连接展示用的 schema 名（没有时为空）。
func (a *root) schemaDisplayName(connID string) string {
	schema := a.connSchema[connID]
	if schema == "" || a.store == nil {
		return schema
	}
	p, ok := a.store.Get(connID)
	if !ok {
		return schema
	}
	return schemaLabel(p.Kind, p.Database, schema)
}
