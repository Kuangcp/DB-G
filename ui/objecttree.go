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
	if a.con.Session() == nil || a.activeConnID != p.ID {
		_ = a.openConn(p)
	}
}

func (a *root) loadGroups(p app.ConnProfile) {
	key := p.ID
	if _, ok := a.treeData[key]; ok {
		return
	}
	sess := a.con.Session()
	if sess == nil || a.activeConnID != p.ID {
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
	for _, g := range objectKinds {
		objs, err := lister.Objects(ctx, g.kind)
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
	if _, ok := a.treeData[p.ID]; !ok {
		ui.Text(c, "加载中…").TextColor(c.Theme().TextMuted)
		a.loadGroups(p)
		if err := a.treeErr[p.ID]; err != "" {
			ui.Text(c, err).TextColor(c.Theme().Danger)
			return
		}
	}
	data := a.treeData[p.ID]
	ui.Tree(c, func() {
		for _, g := range objectKinds {
			objs := data[g.kind]
			label := g.label
			if _, loaded := a.treeData[p.ID]; loaded {
				label = fmt.Sprintf("%s (%d)", g.label, len(objs))
			}
			open := a.groupOpenPtr(p.ID+"|"+kindKey(g.kind), false)
			ui.TreeItem(c, label, open, func() {
				for _, o := range objs {
					if ui.TreeItem(c, o.Name, nil, nil).Selected(a.treeSelected == p.ID+"|"+o.Name).Clicked() {
						a.treeSelected = p.ID + "|" + o.Name
					}
				}
			})
		}
	})
}
