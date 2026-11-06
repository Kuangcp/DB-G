package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

var objectKinds = []struct {
	kind  engine.ObjectKind
	label string
}{
	{engine.ObjectTable, "表"},
	{engine.ObjectView, "视图"},
	{engine.ObjectIndex, "索引"},
}

func kindKey(k engine.ObjectKind) string {
	switch k {
	case engine.ObjectTable:
		return "table"
	case engine.ObjectView:
		return "view"
	default:
		return "index"
	}
}

func (a *root) groupOpenPtr(key string, v bool) *bool {
	if a.treeOpen == nil {
		a.treeOpen = map[string]*bool{}
	}
	if p, ok := a.treeOpen[key]; ok {
		return p
	}
	p := new(bool)
	*p = v
	a.treeOpen[key] = p
	return p
}

func (a *root) reloadTree() {
	a.treeData = nil
	a.treeErr = nil
}

func (a *root) setTreeErr(key, msg string) {
	if a.treeErr == nil {
		a.treeErr = map[string]string{}
	}
	a.treeErr[key] = msg
}

func (a *root) expandConn(p app.ConnProfile) {
	if a.sess == nil || a.sessConnID != p.ID {
		_ = a.openConn(p)
	}
}

func (a *root) toggleExpandConn(p app.ConnProfile) {
	if a.treeExpand == nil {
		a.treeExpand = map[string]bool{}
	}
	a.treeExpand[p.ID] = !a.treeExpand[p.ID]
	if a.treeExpand[p.ID] {
		if a.treeErr != nil {
			delete(a.treeErr, p.ID)
		}
		a.expandConn(p)
	}
}

func (a *root) loadGroupsIn(p app.ConnProfile, schema string) {
	key := p.ID
	if schema != "" {
		key = p.ID + "|" + schema
	}
	if _, ok := a.treeData[key]; ok {
		return
	}
	sess := a.sess
	if sess == nil || a.sessConnID != p.ID {
		a.setTreeErr(key, "未连接")
		return
	}
	if !sess.Caps().Has(engine.CapObjects) {
		a.setTreeErr(key, "该数据源不支持对象树")
		return
	}
	lister, ok := sess.(engine.ObjectLister)
	if !ok {
		a.setTreeErr(key, "该数据源不支持对象树")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data := map[engine.ObjectKind][]engine.DBObject{}
	scoped, multi := a.sess.(engine.SchemaLister)
	for _, g := range objectKinds {
		var objs []engine.DBObject
		var err error
		if schema != "" && multi {
			objs, err = scoped.ObjectsIn(ctx, schema, g.kind)
		} else {
			objs, err = lister.Objects(ctx, g.kind)
		}
		if err != nil {
			a.setTreeErr(key, err.Error())
			return
		}
		data[g.kind] = objs
	}
	if a.treeData == nil {
		a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{}
	}
	a.treeData[key] = data
}

func (a *root) objectGroups(c *ui.Context, p app.ConnProfile) {
	if err := a.treeErr[p.ID]; err != "" {
		ui.Text(c, err).TextColor(c.Theme().Danger)
		return
	}
	// 多 schema 数据源：先列 schema，再在 schema 下列对象分组。
	if lister, ok := a.schemaLister(p); ok {
		a.schemaTree(c, p, lister)
		return
	}
	a.loadGroupsIn(p, "")
	data := a.treeData[p.ID]
	ui.Tree(c, func() {
		a.kindGroups(c, p, "", data)
	})
}

// schemaLister 返回该连接当前会话的 SchemaLister（未连接/不支持时为 false）。
func (a *root) schemaLister(p app.ConnProfile) (engine.SchemaLister, bool) {
	if a.sess == nil || a.sessConnID != p.ID {
		return nil, false
	}
	lister, ok := a.sess.(engine.SchemaLister)
	return lister, ok
}

// schemaTree 渲染 schema 列表（懒加载）。
func (a *root) schemaTree(c *ui.Context, p app.ConnProfile, lister engine.SchemaLister) {
	if a.treeSchemas == nil {
		a.treeSchemas = map[string][]string{}
	}
	if _, ok := a.treeSchemas[p.ID]; !ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		schemas, err := lister.Schemas(ctx)
		cancel()
		if err != nil {
			ui.Text(c, err.Error()).TextColor(c.Theme().Danger)
			return
		}
		a.treeSchemas[p.ID] = schemas
	}
	ui.Tree(c, func() {
		for _, schema := range a.treeSchemas[p.ID] {
			key := p.ID + "|" + schema
			open := a.groupOpenPtr(key+"|schema", false)
			ui.TreeItem(c, "▣ "+schema, open, func() {
				a.loadGroupsIn(p, schema)
				a.kindGroups(c, p, schema, a.treeData[key])
			})
		}
	})
}

// kindGroups 渲染某个 schema（或默认 schema，schema=""）下的对象分组。
func (a *root) kindGroups(c *ui.Context, p app.ConnProfile, schema string, data map[engine.ObjectKind][]engine.DBObject) {
	base := p.ID
	if schema != "" {
		base = p.ID + "|" + schema
	}
	for _, g := range objectKinds {
		objs := data[g.kind]
		label := fmt.Sprintf("%s (%d)", g.label, len(objs))
		open := a.groupOpenPtr(base+"|"+kindKey(g.kind), false)
		ui.TreeItem(c, label, open, func() {
			for _, o := range objs {
				obj := o
				selKey := base + "|" + obj.Name
				leaf := ui.TreeItem(c, obj.Name, nil, nil).Selected(a.treeSelected == selKey)
				if leaf.Clicked() {
					a.treeSelected = selKey
				}
				if leaf.DoubleClicked() {
					a.previewObject(p, obj)
				}
				leaf.ContextMenu(func(m *ui.Menu) {
					if m.Item("预览数据").Chosen() {
						a.previewObject(p, obj)
					}
					if m.Item("查看 DDL").Chosen() {
						a.showDDL(p, obj)
					}
					if m.Item("插入名称").Chosen() {
						a.code.Insert(obj.Name)
					}
				})
			}
		})
	}
}

// previewObject 在新控制台里预览对象数据（SELECT * … LIMIT 200）并立即执行。
func (a *root) previewObject(p app.ConnProfile, o engine.DBObject) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if a.sessConnID != p.ID {
		if err := a.openConn(p); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	rec, err := m.Create(p.ID, "预览 "+o.Name)
	if err != nil {
		a.consoleErr = "新建预览控制台失败: " + err.Error()
		return
	}
	a.pendingActivate = rec.ID
	a.pendingText = "SELECT * FROM " + qualifiedName(p.Kind, o) + " LIMIT 200"
	a.pendingExec = true
}

// showDDL 取对象的 DDL 并弹窗（headless 同步，有窗口时后台 + Update）。
func (a *root) showDDL(p app.ConnProfile, o engine.DBObject) {
	if a.sessConnID != p.ID {
		if err := a.openConn(p); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	load := func() {
		lister, ok := a.sess.(engine.ObjectDDLer)
		if !ok || !a.sess.Caps().Has(engine.CapObjectDDL) {
			a.ddlTitle, a.ddlText, a.ddlOpen = o.Name, "该数据源不支持查看 DDL", true
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ddl, err := lister.ObjectDDL(ctx, o.Kind, o.Name)
		apply := func() {
			a.ddlTitle, a.ddlOpen = o.Name, true
			if err != nil {
				a.ddlText = "读取 DDL 失败: " + err.Error()
				app.LogWarn("ddl", "load failed", "object", o.Name, "err", err.Error())
				return
			}
			a.ddlText = ddl
		}
		if a.win != nil {
			win := a.win
			win.Update(apply)
			return
		}
		apply()
	}
	if a.win == nil {
		load()
		return
	}
	go load()
}

// ddlPanel 渲染 DDL 弹窗。
func (a *root) ddlPanel(c *ui.Context) {
	if !a.ddlOpen {
		return
	}
	ui.Modal(c, &a.ddlOpen, func() {
		ui.Text(c, "DDL："+a.ddlTitle).Bold().SingleLine()
		ui.ScrollBoth(c).Height(360).Children(func() {
			ui.Text(c, a.ddlText).Font("monospace").FontSize(12)
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "复制").Clicked() {
				c.WriteClipboard(a.ddlText)
			}
			if ui.PrimaryButton(c, "关闭").Clicked() {
				a.ddlOpen = false
			}
		})
	})
}

// qualifiedName 按对象所在的 schema 生成引用名（跨 schema 预览/DDL 用）。
func qualifiedName(kind string, o engine.DBObject) string {
	if o.Schema != "" {
		return app.QuoteIdent(kind, o.Schema) + "." + app.QuoteIdent(kind, o.Name)
	}
	return app.QuoteIdent(kind, o.Name)
}
