package ui

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

func (a *root) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		ui.Toolbar(c, func() {
			if iconButton(c, iconPlug, locales.SidebarNewConn(), ui.Transparent).Clicked() {
				a.openNewConnForm("")
			}
			if iconButton(c, iconFolderPlus, locales.SidebarNewFolder(), ui.Transparent).Clicked() {
				a.openFolderModal("new", "", "")
			}
			if iconButton(c, iconRefresh, locales.SidebarReloadTree(), ui.Transparent).Clicked() {
				a.reloadTree()
			}
			ui.Spacer(c)
			if iconButtonPlain(c, iconPanelLeftClose, locales.SidebarHide(), ui.Transparent).Clicked() {
				a.sidebarHidden = true
			}
		}).Label(locales.SidebarDatasourceActions()).DragWindow()
		if a.store == nil {
			ui.Text(c, locales.SidebarStartHint()).TextColor(c.Theme().TextMuted)
			a.connModal(c)
			a.folderModal(c)
			return
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c, &a.tree.ObjSearch).Placeholder(locales.SidebarSearchPlaceholder()).Label(locales.SidebarSearchObjects()).Fill()
			if a.tree.ObjSearch != "" {
				if iconButtonPlain(c, iconClose, locales.SidebarClearSearch(), ui.Transparent).Clicked() {
					a.tree.ObjSearch = ""
				}
			}
		})
		if a.objSearchActive() {
			ui.Text(c, locales.SidebarSearchLoadedOnly()).FontSize(11.5).TextColor(c.Theme().TextMuted)
		}
		if w := a.store.Warning(); w != "" {
			ui.Text(c, w).TextColor(c.Theme().Danger)
		}
		if len(a.store.List()) == 0 && len(a.store.Folders()) == 0 {
			ui.Text(c, locales.SidebarStartHint()).TextColor(c.Theme().TextMuted)
		}
		ui.Scroll(c).Grow(1).Children(func() {
			a.connTree(c)
		})
	})
	a.connModal(c)
	a.folderModal(c)
}

// connTreeIndex 是一帧内的连接树索引：父级 → 子项、ID → 记录。
// 数据量是几十个量级，每帧重建足够快，也避免缓存失效。
type connTreeIndex struct {
	foldersByParent map[string][]app.ConnFolder
	connsByFolder   map[string][]app.ConnProfile
	folders         map[string]app.ConnFolder
	conns           map[string]app.ConnProfile
}

func (a *root) treeIndex() *connTreeIndex {
	idx := &connTreeIndex{
		foldersByParent: map[string][]app.ConnFolder{},
		connsByFolder:   map[string][]app.ConnProfile{},
		folders:         map[string]app.ConnFolder{},
		conns:           map[string]app.ConnProfile{},
	}
	for _, f := range a.store.Folders() {
		idx.foldersByParent[f.ParentID] = append(idx.foldersByParent[f.ParentID], f)
		idx.folders[f.ID] = f
	}
	for _, p := range a.store.List() {
		idx.connsByFolder[p.FolderID] = append(idx.connsByFolder[p.FolderID], p)
		idx.conns[p.ID] = p
	}
	return idx
}

func (idx *connTreeIndex) folderWithin(rootID, id string) bool {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		if id == rootID {
			return true
		}
		seen[id] = true
		f, ok := idx.folders[id]
		if !ok {
			return false
		}
		id = f.ParentID
	}
	return false
}

// connTree 渲染连接列表：同层先文件夹、后连接；文件夹可递归嵌套。
func (a *root) connTree(c *ui.Context) {
	if a.store == nil {
		return
	}
	a.connBranch(c, "", 0, a.treeIndex())
}

func (a *root) connBranch(c *ui.Context, parent string, depth int, idx *connTreeIndex) {
	searching := a.objSearchActive()
	for _, f := range idx.foldersByParent[parent] {
		if searching && !a.folderMatches(idx, f.ID) {
			continue
		}
		a.folderRow(c, f, depth, idx)
	}
	for _, p := range idx.connsByFolder[parent] {
		if searching && !a.connMatches(p) {
			continue
		}
		a.connRow(c, p, depth, idx)
	}
}

// objSearchActive 报告对象树搜索是否生效。
func (a *root) objSearchActive() bool { return strings.TrimSpace(a.tree.ObjSearch) != "" }

// objMatch 报告名字是否命中当前搜索词（大小写不敏感）。
func (a *root) objMatch(name string) bool {
	q := strings.ToLower(strings.TrimSpace(a.tree.ObjSearch))
	return q != "" && strings.Contains(strings.ToLower(name), q)
}

// connObjectMatches 报告连接已加载的对象里是否有命中搜索词的对象。
func (a *root) connObjectMatches(connID string) bool {
	for base, m := range a.tree.Data {
		if base != connID && !strings.HasPrefix(base, connID+"|") {
			continue
		}
		for _, objs := range m {
			for _, o := range objs {
				if a.objMatch(o.Name) {
					return true
				}
			}
		}
	}
	return false
}

// searchFiltersObjects 报告搜索时是否要过滤该数据源的对象：数据源名本身命中时
// 展示完整子树，只有对象命中时才按对象名过滤。
func (a *root) searchFiltersObjects(p app.ConnProfile) bool {
	return a.objSearchActive() && !a.objMatch(p.Name)
}

// connMatches 报告数据源是否应在搜索结果里显示（名称命中或已加载对象命中）。
func (a *root) connMatches(p app.ConnProfile) bool {
	return a.objMatch(p.Name) || a.connObjectMatches(p.ID)
}

// folderMatches 报告文件夹或其子孙里是否有命中（文件夹名本身也算）。
func (a *root) folderMatches(idx *connTreeIndex, folderID string) bool {
	if f, ok := idx.folders[folderID]; ok && a.objMatch(f.Name) {
		return true
	}
	for _, f := range idx.foldersByParent[folderID] {
		if a.folderMatches(idx, f.ID) {
			return true
		}
	}
	for _, p := range idx.connsByFolder[folderID] {
		if a.connMatches(p) {
			return true
		}
	}
	return false
}

// schemaHasMatch 报告某 schema 下已加载对象是否有命中。
func (a *root) schemaHasMatch(connID, schema string) bool {
	base := objectTreeKey(connID, schema)
	for _, objs := range a.tree.Data[base] {
		for _, o := range objs {
			if a.objMatch(o.Name) {
				return true
			}
		}
	}
	return false
}

// folderIndent 是每层文件夹/连接的缩进宽度（DIP）。
const folderIndent = 14

// treeRowBase 是侧栏树/控制台的一行：没有按钮外观，悬停浅底，整行可点。
// 有 Spacer 的行会铺满可用宽度。
func treeRowBase(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	row := ui.ButtonBase(c).Label(label).Gap(6).AlignItems(ui.Center).
		Padding(t.Space(0.75), t.Space(1)).Radius(t.Space(1)).FillWidth()
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	return row
}

// treeArrow 是树行的展开箭头：统一 Lucide 描线，标签带名称，读屏和测试都能定位。
func treeArrow(c *ui.Context, label string, open bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(t.Space(0.5)).Radius(t.Space(1))
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ic := ui.Icon(c, iconChevronDown).FontSize(14)
		if !open {
			ic.Rotate(-90)
		}
		ic.TextColor(t.TextMuted)
	})
	return b
}

// treeGroupRow 是对象树里的可折叠分组行：箭头 + 图标 + 文案 + 可选尾部控件。
// 整行可点，箭头也能单独展开/收起。
func treeGroupRow(c *ui.Context, label string, open *bool, depth int, icon *ui.SVG, iconColor ui.Color, trailing func()) ui.Element {
	row := treeRowBase(c, label).PaddingStart(float32(4 + depth*folderIndent))
	row.Children(func() {
		arrow := locales.FolderExpand(label)
		if *open {
			arrow = locales.FolderCollapse(label)
		}
		if treeArrow(c, arrow, *open).Clicked() {
			*open = !*open
		}
		if icon != nil {
			ui.Icon(c, icon).FontSize(15).TextColor(iconColor)
		}
		ui.Text(c, label).SingleLine()
		ui.Spacer(c)
		if trailing != nil {
			trailing()
		}
	})
	if row.Clicked() {
		*open = !*open
	}
	return row
}

// treeInfoRow 是树里的提示行（加载中/错误/已显示上限），不可折叠。
func treeInfoRow(c *ui.Context, text string, depth int, color ui.Color) {
	ui.Text(c, text).SingleLine().FontSize(12).TextColor(color).
		PaddingStart(float32(4 + depth*folderIndent))
}

// treeDrag 是侧栏拖拽的值：kind 区分数据源和文件夹。
type treeDrag struct{ kind, id string }

const (
	treeDragConn   = "conn"
	treeDragFolder = "folder"
)

// attachTreeDrag 只在按住 Ctrl 时把行接成拖拽源，避免普通点击误拖；
// 已经开始拖的行即使松开 Ctrl 也继续保留拖拽源，直到放手。
func attachTreeDrag(c *ui.Context, row ui.Element, v treeDrag) {
	held := c.Modifiers()&ui.Ctrl != 0
	if !held && !row.Dragging() {
		return
	}
	row.Drag(v)
	if held {
		row.Cursor(ui.CursorGrab).Tooltip(locales.SidebarTreeDragTip())
	}
	if row.Dragging() {
		row.Opacity(0.45)
	}
}

type dropSpot uint8

const (
	dropNone dropSpot = iota
	dropBefore
	dropAfter
	dropInto
)

// treeDropSpot 根据指针在行内的位置决定插入到前面/后面，还是移入文件夹。
func treeDropSpot(row ui.Element, targetKind, targetID string, drag treeDrag, idx *connTreeIndex) dropSpot {
	switch drag.kind {
	case treeDragFolder:
		if targetKind != "folder" || idx.folderWithin(drag.id, targetID) {
			return dropNone
		}
	case treeDragConn:
		if targetKind == "conn" && drag.id == targetID {
			return dropNone
		}
	default:
		return dropNone
	}
	_, y, ok := row.PointerPosition()
	if !ok {
		return dropNone
	}
	h := row.Bounds().H
	if h <= 0 {
		return dropNone
	}
	if targetKind == "folder" {
		if drag.kind == treeDragConn {
			return dropInto
		}
		switch {
		case y < h*0.28:
			return dropBefore
		case y > h*0.72:
			return dropAfter
		default:
			return dropInto
		}
	}
	if y < h/2 {
		return dropBefore
	}
	return dropAfter
}

func styleTreeDrop(row ui.Element, spot dropSpot, t *ui.Theme) {
	switch spot {
	case dropInto:
		row.Background(t.Accent.Alpha(0.12))
	case dropBefore:
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, t.Accent, 1)
		})
	case dropAfter:
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 2, W: r.W, H: 2}, t.Accent, 1)
		})
	}
}

func nextFolderSibling(idx *connTreeIndex, f app.ConnFolder) string {
	sib := idx.foldersByParent[f.ParentID]
	for i := range sib {
		if sib[i].ID == f.ID && i+1 < len(sib) {
			return sib[i+1].ID
		}
	}
	return ""
}

func nextConnSibling(idx *connTreeIndex, p app.ConnProfile) string {
	sib := idx.connsByFolder[p.FolderID]
	for i := range sib {
		if sib[i].ID == p.ID && i+1 < len(sib) {
			return sib[i+1].ID
		}
	}
	return ""
}

func (a *root) applyTreeDrop(drag treeDrag, targetKind, targetID string, spot dropSpot, idx *connTreeIndex) {
	if a.store == nil {
		return
	}
	var parent, before string
	switch targetKind {
	case "folder":
		f, ok := idx.folders[targetID]
		if !ok {
			return
		}
		switch spot {
		case dropInto:
			parent, before = targetID, ""
		case dropBefore:
			parent, before = f.ParentID, targetID
		case dropAfter:
			parent, before = f.ParentID, nextFolderSibling(idx, f)
		default:
			return
		}
	case "conn":
		p, ok := idx.conns[targetID]
		if !ok {
			return
		}
		switch spot {
		case dropBefore:
			parent, before = p.FolderID, targetID
		case dropAfter:
			parent, before = p.FolderID, nextConnSibling(idx, p)
		default:
			return
		}
	default:
		return
	}
	var err error
	switch drag.kind {
	case treeDragConn:
		err = a.store.MoveConnectionTo(drag.id, parent, before)
	case treeDragFolder:
		err = a.store.MoveFolderTo(drag.id, parent, before)
	default:
		return
	}
	if err != nil {
		app.LogWarn("conn", "tree move failed", "kind", drag.kind, "id", drag.id,
			"parent", parent, "before", before, "err", err.Error())
		return
	}
	if spot == dropInto && targetKind == "folder" {
		delete(a.tree.FolderCollapsed, targetID) // 移入后展开，结果立即可见
	}
	app.LogInfo("conn", "tree moved", "kind", drag.kind, "id", drag.id, "parent", parent, "before", before)
}

// attachTreeDrop 把行接成拖拽目标：上下插入线，文件夹中间高亮表示移入。
func (a *root) attachTreeDrop(c *ui.Context, row ui.Element, targetKind, targetID string, idx *connTreeIndex) {
	if drag, over := ui.DragOver[treeDrag](row); over {
		if spot := treeDropSpot(row, targetKind, targetID, drag, idx); spot != dropNone {
			styleTreeDrop(row, spot, c.Theme())
		}
	}
	if drag, ok := ui.Drop[treeDrag](row); ok {
		if spot := treeDropSpot(row, targetKind, targetID, drag, idx); spot != dropNone {
			a.applyTreeDrop(drag, targetKind, targetID, spot, idx)
		}
	}
}

func (a *root) folderRow(c *ui.Context, f app.ConnFolder, depth int, idx *connTreeIndex) {
	collapsed := a.tree.FolderCollapsed[f.ID] && !a.objSearchActive()
	row := treeRowBase(c, locales.FolderRow(f.Name)).PaddingStart(float32(4 + depth*folderIndent))
	row.Children(func() {
		var label string
		if collapsed {
			label = locales.FolderExpand(f.Name)
		} else {
			label = locales.FolderCollapse(f.Name)
		}
		if treeArrow(c, label, !collapsed).Clicked() {
			a.toggleFolder(f.ID)
		}
		folderIcon := iconFolder
		if !collapsed {
			folderIcon = iconFolderOpen
		}
		ui.Icon(c, folderIcon).FontSize(15).TextColor(c.Theme().TextMuted)
		ui.Text(c, f.Name).SingleLine()
		if n := len(idx.foldersByParent[f.ID]) + len(idx.connsByFolder[f.ID]); n > 0 && collapsed {
			ui.Text(c, strconv.Itoa(n)).FontSize(10.5).TextColor(c.Theme().TextMuted)
		}
		ui.Spacer(c)
	})
	if row.Clicked() {
		a.toggleFolder(f.ID)
	}
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item(locales.FolderMenuNew()).Chosen() {
			a.openFolderModal("new", f.ID, "")
		}
		if menu.Item(locales.FolderMenuNewConn()).Chosen() {
			a.openNewConnForm(f.ID)
		}
		if menu.Item(locales.TabMenuRename()).Chosen() {
			a.openFolderModal("rename", f.ID, f.Name)
		}
		if menu.Item(locales.TabMenuDelete()).Chosen() {
			a.openFolderModal("delete", f.ID, f.Name)
		}
	})
	attachTreeDrag(c, row, treeDrag{kind: treeDragFolder, id: f.ID})
	a.attachTreeDrop(c, row, "folder", f.ID, idx)
	if !collapsed {
		a.connBranch(c, f.ID, depth+1, idx)
	}
}

func (a *root) toggleFolder(id string) {
	if a.tree.FolderCollapsed == nil {
		a.tree.FolderCollapsed = map[string]bool{}
	}
	a.tree.FolderCollapsed[id] = !a.tree.FolderCollapsed[id]
}

func (a *root) connRow(c *ui.Context, p app.ConnProfile, depth int, idx *connTreeIndex) {
	live := a.connected(p.ID)
	expanded := a.tree.Expand[p.ID] || a.objSearchActive()
	row := treeRowBase(c, locales.ConnRow(p.Name)).PaddingStart(float32(4 + depth*folderIndent))
	row.Children(func() {
		var label string
		if expanded {
			label = locales.ConnCollapse(p.Name)
		} else {
			label = locales.ConnExpand(p.Name)
		}
		if treeArrow(c, label, expanded).Clicked() {
			a.toggleExpandConn(p)
		}
		if icon := connKindIcon(p.Kind); icon != nil {
			ui.Icon(c, icon).FontSize(15).TextColor(connKindColor(p.Kind))
		}
		name := ui.Text(c, p.Name).SingleLine()
		if live {
			name.Bold()
		}
		ui.Spacer(c)
		a.connDot(c, p.ID)
		if iconButtonPlain(c, iconSquarePen, locales.ConnEdit(), ui.Transparent).Clicked() {
			a.connForm.Open, a.connForm.ID, a.connForm.Name, a.connForm.Path, a.connForm.Err = true, p.ID, p.Name, p.Path, ""
			a.connForm.URL, a.connForm.URLErr = "", ""
			a.connForm.Kind = p.Kind
			a.connForm.Host, a.connForm.User, a.connForm.Pass, a.connForm.DB = p.Host, p.User, p.Password, p.Database
			a.connForm.ShowPass = false
			a.connForm.Port = ""
			if p.Port > 0 {
				a.connForm.Port = strconv.Itoa(p.Port)
			}
			a.connForm.Params = p.Params
			a.connForm.Timeout = ""
			if p.QueryTimeout > 0 {
				a.connForm.Timeout = strconv.Itoa(p.QueryTimeout)
			}
			a.connForm.Guard = p.GuardBlock
			a.connForm.AIStrict = p.AIContextStrict
			a.connProto = connProtoIndex(p.Protocol)
			a.connForm.Folder = p.FolderID
			a.setConnTagForm(p.Tags)
			a.resetConnTest()
		}
	})
	// 刷新元数据 / 复制数据源 / 删除连接是低频且易误触的操作：放右键菜单，不占行内位置。
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item(locales.ConnRefreshMeta()).Chosen() {
			a.refreshConnMeta(p.ID)
		}
		if menu.Item(locales.ConnMenuDuplicate()).Chosen() {
			a.duplicateConn(p)
		}
		menu.Separator()
		if menu.Item(locales.ConnMenuDelete()).Chosen() {
			a.connForm.Deleting, a.connForm.DeleteOpen = p.ID, true
		}
	})
	// 点数据源名/行就是「连上它并展开」：启动时不预连，连接时机只有这一处。
	if row.Clicked() {
		a.expandConnAndTree(p)
	}
	attachTreeDrag(c, row, treeDrag{kind: treeDragConn, id: p.ID})
	a.attachTreeDrop(c, row, "conn", p.ID, idx)
	if expanded {
		ui.Column(c).PaddingStart(float32((depth + 1) * folderIndent)).Children(func() {
			a.consoleSection(c, p)
			a.objectGroups(c, p, 0)
		})
	}
}

// duplicateConn 在同文件夹下复制一份数据源：名称自动取未被占用的「副本：X」，
// 只落盘一次（Add 内部生成新 ID）。
func (a *root) duplicateConn(p app.ConnProfile) {
	if a.store == nil {
		a.toastError(locales.ConnDupStoreUnavailable())
		return
	}
	taken := make(map[string]bool)
	for _, q := range a.store.List() {
		taken[q.Name] = true
	}
	name := locales.ConnDupName(p.Name)
	for i := 1; taken[name]; i++ {
		name = locales.ConnDupNameN(p.Name, i)
	}
	cloned := app.ConnProfile{
		Name:            name,
		Kind:            p.Kind,
		Path:            p.Path,
		Host:            p.Host,
		Port:            p.Port,
		User:            p.User,
		Password:        p.Password,
		Database:        p.Database,
		Protocol:        p.Protocol,
		Params:          p.Params,
		FolderID:        p.FolderID,
		QueryTimeout:    p.QueryTimeout,
		AIContextStrict: p.AIContextStrict,
		Tags:            p.Tags,
	}
	if err := a.store.Add(cloned); err != nil {
		a.toastError(locales.ConnDupFailed(err.Error()))
		return
	}
	app.LogInfo("conn", "duplicate", "from", p.Name, "to", name, "folder", p.FolderID)
}

// consoleSection 是数据源下的控制台汇总：展开后列出该数据源的全部控制台（打开与否两种状态）。
func (a *root) consoleSection(c *ui.Context, p app.ConnProfile) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	recs := m.ConsolesOf(p.ID)
	open := a.groupOpen(p.ID+"|consoles", true)
	treeGroupRow(c, locales.ConsoleSection(), open, 0, iconTerminal, c.Theme().TextMuted, func() {
		if !*open && len(recs) > 0 {
			ui.Text(c, strconv.Itoa(len(recs))).FontSize(10.5).TextColor(c.Theme().TextMuted)
		}
		if iconButtonPlain(c, iconPlus, locales.ConsoleNew(), ui.Transparent).Clicked() {
			rec, err := m.Create(p.ID, "")
			if err != nil {
				a.reportBackgroundError(locales.ConsoleCreateFailed(err.Error()))
				return
			}
			a.deferActivateConsole(rec.ID, "")
		}
	})
	if !*open {
		return
	}
	for _, rec := range recs {
		a.consoleRow(c, rec, m.IsOpen(rec.ID), 1)
	}
}

// consoleRow 渲染一个控制台：已打开加粗，点击打开或激活，右键关闭/重命名/删除。
func (a *root) consoleRow(c *ui.Context, rec app.ConsoleRecord, isOpen bool, depth int) {
	row := treeRowBase(c, locales.ConsoleRow(rec.Name)).PaddingStart(float32(4 + depth*folderIndent))
	row.Children(func() {
		dot := ui.Text(c, "●")
		if isOpen {
			dot.TextColor(c.Theme().Success)
		} else {
			dot.TextColor(c.Theme().TextMuted)
		}
		name := ui.Text(c, rec.Name).SingleLine()
		if isOpen {
			name.Bold()
		} else {
			name.TextColor(c.Theme().TextMuted)
		}
		ui.Spacer(c)
	})
	if row.Clicked() {
		a.deferActivateConsole(rec.ID, "")
	}
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item(locales.TabMenuClose()).Disabled(!isOpen).Chosen() {
			a.deferCloseConsole(rec.ID)
		}
		if menu.Item(locales.TabMenuRename()).Chosen() {
			a.openModal("rename", rec.ID, rec.Name)
		}
		if menu.Item(locales.TabMenuDelete()).Chosen() {
			a.openModal("delete", rec.ID, rec.Name)
		}
	})
}

// 连接弹窗的固定尺寸：切换数据源时表单行数不同，面板却不变，窗口不会一弹一弹。
const (
	connFormWidth  = 840
	connFormHeight = 600
)

func (a *root) connModal(c *ui.Context) {
	if a.connForm.DeleteOpen {
		a.deleteModal(c)
		return
	}
	ui.Modal(c, &a.connForm.Open, func() {
		radius := controlRadius(c)
		title := locales.ConnModalNew()
		if a.connForm.ID != "" {
			title = locales.ConnModalEdit()
		}
		ui.Text(c, title).Bold()
		// 左右布局：左侧数据源列表（无按钮外观、行间分隔线、选中高亮），右侧表单。
		ui.Row(c).Fill().Gap(0).Children(func() {
			ui.Column(c).Width(168).FillHeight().Children(func() {
				for _, m := range connKindMetas() {
					a.connKindRow(c, m)
				}
				ui.Spacer(c)
			})
			ui.Box(c).Width(1).FillHeight().Background(c.Theme().Border)
			ui.Column(c).Fill().Padding(0, 16).Gap(8).Children(func() {
				ui.Text(c, locales.ConnFormTitle(kindLabel(a.connForm.Kind))).Bold()
				ui.TextInput(c, &a.connForm.Name).Placeholder(locales.ConnFormName()).Radius(radius).AutoFocus()
				if a.store != nil && len(a.store.Folders()) > 0 {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, locales.ConnFormLocation()).FontSize(12).TextColor(c.Theme().TextMuted)
						a.folderSelect(c)
						ui.Spacer(c)
					})
				}
				if a.connForm.Kind != "sqlite" {
					ui.TextInput(c, &a.connForm.DB).Placeholder(locales.ConnFormDatabase()).Radius(radius)
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connForm.Host).Placeholder(locales.ConnFormHost()).Radius(radius).Fill()
						ui.TextInput(c, &a.connForm.Port).Placeholder(locales.ConnFormPort()).Radius(radius)
					})
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connForm.User).Placeholder(locales.ConnFormUser()).Radius(radius).Fill()
						a.connPasswordInput(c, radius)
					})
					if a.connForm.Kind == "clickhouse" {
						// 协议决定 DSN 的 scheme，也决定默认端口：native 9000、
						// HTTP 8123、HTTPS 8443。填错端口会握手失败。
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, locales.ConnFormProtocol()).FontSize(12).TextColor(c.Theme().TextMuted)
							if ui.Segmented(c, &a.connProto, "Native", "HTTP", "HTTPS").Radius(radius).Changed() {
								a.applyConnProto()
							}
							ui.Spacer(c)
						})
					}
					ui.TextInput(c, &a.connForm.Params).Placeholder(paramsPlaceholder(a.connForm.Kind)).Radius(radius)
				} else {
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connForm.Path).Placeholder(locales.ConnFormSqlitePath()).Radius(radius).Fill()
						if ui.Button(c, locales.ConnFormBrowse()).Radius(radius).Clicked() {
							a.browsePath()
						}
					})
				}
				ui.TextInput(c, &a.connForm.Timeout).
					Placeholder(locales.ConnFormTimeout()).Radius(radius)
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					full := len(a.connForm.Tags) >= app.MaxConnTags
					ui.Text(c, locales.ConnFormTags()).FontSize(12).TextColor(c.Theme().TextMuted).
						Tooltip(locales.ConnFormTagsMax())
					swatch := ui.Button(c, "●").FontSize(13).TextColor(tagColor(a.connForm.TagColor)).
						Radius(radius).Padding(2, 8).Disabled(full)
					swatch.Tooltip(locales.ConnFormTagColor(tagColorLabel(a.connForm.TagColor)))
					if swatch.Clicked() {
						a.connForm.TagColor = nextTagColor(a.connForm.TagColor)
					}
					placeholder := locales.ConnFormTagPlaceholder()
					if full {
						placeholder = locales.ConnFormTagsFullPlaceholder()
					}
					cb := ui.ComboboxBase(c, &a.connForm.TagInput)
					cb.Input.Placeholder(placeholder).Radius(radius).Fill().
						Label(locales.ConnFormAddTag()).AutoComplete(ui.AutoCompleteList).Disabled(full)
					cb.Popup(func(panel ui.Element) {
						theme := c.Theme()
						panel.Padding(4).Radius(radius).Background(theme.Background).Border(1, theme.Border)
						suggestions := a.tagSuggestions()
						if len(suggestions) == 0 {
							ui.Text(c, locales.ConnFormNoTags()).FontSize(11.5).
								TextColor(theme.TextMuted).Padding(4, 8)
							return
						}
						for _, tag := range suggestions {
							item := cb.Item(tag.Label).Padding(3, 8).Radius(radius)
							if item.Highlighted() {
								item.Background(theme.Accent.Alpha(0.12))
							}
							item.Children(func() {
								ui.Text(c, "●").FontSize(8).TextColor(tagColor(tag.Color))
								ui.Text(c, tag.Label).FontSize(12).SingleLine()
								ui.Spacer(c)
							})
						}
					})
					switch chosen, ok := cb.Chosen(); {
					case ok:
						a.addConnFormTag(chosen, a.tagColorByLabel(chosen))
					case cb.Input.Submitted() && !full:
						a.addConnFormTag(a.connForm.TagInput, a.connForm.TagColor)
					}
					// 删除要等标签都画完再改切片，否则 range 期间缩短会越界。
					removed := ""
					for _, tag := range a.connForm.Tags {
						tag := tag
						editTagChip(c, tag, func() { removed = tag.label })
					}
					if removed != "" {
						a.removeConnFormTag(removed)
					}
				})
				ui.Checkbox(c, &a.connForm.Guard, locales.ConnFormBlockGuard())
				ui.Checkbox(c, &a.connForm.AIStrict, locales.ConnFormAIStrict()).Tooltip(locales.ConnFormAIStrictTip())

				if a.connForm.Err != "" {
					ui.Text(c, a.connForm.Err).TextColor(c.Theme().Danger)
				}
				// 留白：按钮沉到面板底部，上方空着。
				ui.Spacer(c)
				if a.connForm.TestMsg != "" {
					col := c.Theme().Success
					if a.connForm.TestErr {
						col = c.Theme().Danger
					}
					ui.Text(c, a.connForm.TestMsg).FontSize(12).TextColor(col).
						SingleLine().Tooltip(a.connForm.TestMsg)
				}
				if a.connForm.URLErr != "" {
					ui.Text(c, a.connForm.URLErr).FontSize(11).TextColor(c.Theme().Danger).
						SingleLine().Tooltip(a.connForm.URLErr)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if a.connForm.TestBusy {
						if ui.Button(c, locales.ConnFormCancelTest()).Radius(radius).Clicked() {
							a.cancelConnTest()
						}
						ui.Spacer(c)
						return
					}
					if ui.Button(c, locales.ConnFormTest()).Radius(radius).Clicked() {
						a.testConnForm()
					}
					if ui.Button(c, locales.ConnFormCopy()).Radius(radius).Tooltip(locales.ConnFormCopyTip()).
						Clicked() {
						a.copyConnForm(c)
					}
					if ui.Button(c, locales.ConnFormURLParseClipboard()).Radius(radius).Clicked() {
						a.connForm.URL = c.ReadClipboard()
						a.applyConnURL()
					}
					ui.Spacer(c)
					if ui.Button(c, locales.CommonCancel()).Radius(radius).Clicked() {
						a.connForm.Open = false
					}
					if ui.PrimaryButton(c, locales.CommonSave()).Radius(radius).Clicked() {
						a.saveConnForm()
					}
				})
			})
		})
	}).Width(connFormWidth).Height(connFormHeight)
}

// connPasswordInput 是连接密码框：眼睛按钮叠在输入框内部右侧，
// 点击在隐藏与明文之间切换（Password 只在隐藏时调用）。
func (a *root) connPasswordInput(c *ui.Context, radius float32) {
	passwordField(c, &a.connForm.Pass, &a.connForm.ShowPass, radius,
		locales.ConnFormPassword(), locales.ConnFormPassword())
}

// connKindRow 是左侧数据源类型的一行：走通用的侧边导航样式（无按钮外观、
// 行底分隔线、选中高亮）。
func (a *root) connKindRow(c *ui.Context, m engine.DriverMeta) {
	selected := a.connForm.Kind == m.Kind
	row := sideNavRow(c, m.Label, connKindIcon(m.Kind), connKindColor(m.Kind), selected)
	if row.Clicked() {
		a.switchConnKind(m.Kind)
	}
}

// connKindOf 返回某个连接的数据源类型（未知为空）。
func (a *root) connKindOf(connID string) string {
	if a.store == nil || connID == "" {
		return ""
	}
	if p, ok := a.store.Get(connID); ok {
		return p.Kind
	}
	return ""
}

// connKindDefaults 是未链接驱动（如纯 ui 测试）时的元数据兜底；
// 生产构建始终用 engine.DriverMetas()。
var connKindDefaults = []engine.DriverMeta{
	{Kind: "sqlite", Order: 10, Label: "SQLite"},
	{Kind: "mysql", Order: 20, Label: "MySQL", DefaultPort: 3306},
	{Kind: "postgres", Order: 30, Label: "PostgreSQL", DefaultPort: 5432},
	{Kind: "clickhouse", Order: 40, Label: "ClickHouse", DefaultPort: 9000,
		DefaultProtocol: "native", Protocols: []string{"native", "http", "https"},
		ProtocolPorts: map[string]int{"native": 9000, "http": 8123, "https": 8443},
		CountExpr:     "count()"},
}

func connKindMetas() []engine.DriverMeta {
	ms := engine.DriverMetas()
	if len(ms) == 0 {
		return connKindDefaults
	}
	seen := make(map[string]bool, len(ms))
	for i := range ms {
		ms[i] = mergeKindMeta(ms[i])
		seen[ms[i].Kind] = true
	}
	for _, d := range connKindDefaults {
		if !seen[d.Kind] {
			ms = append(ms, d)
		}
	}
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Order != ms[j].Order {
			return ms[i].Order < ms[j].Order
		}
		return ms[i].Label < ms[j].Label
	})
	return ms
}

func defaultKindMeta(kind string) (engine.DriverMeta, bool) {
	for _, d := range connKindDefaults {
		if d.Kind == kind {
			return d, true
		}
	}
	return engine.DriverMeta{}, false
}

// mergeKindMeta 用内置兜底补齐测试桩/旧注册缺失的字段。
func mergeKindMeta(m engine.DriverMeta) engine.DriverMeta {
	def, ok := defaultKindMeta(m.Kind)
	if !ok {
		return m
	}
	if m.Order == 0 {
		m.Order = def.Order
	}
	if m.Label == "" {
		m.Label = def.Label
	}
	if m.DefaultPort == 0 {
		m.DefaultPort = def.DefaultPort
	}
	if m.DefaultProtocol == "" {
		m.DefaultProtocol = def.DefaultProtocol
	}
	if len(m.Protocols) == 0 {
		m.Protocols = def.Protocols
	}
	if len(m.ProtocolPorts) == 0 {
		m.ProtocolPorts = def.ProtocolPorts
	}
	if m.CountExpr == "" {
		m.CountExpr = def.CountExpr
	}
	return m
}

func kindMeta(kind string) engine.DriverMeta {
	if m, ok := engine.Meta(kind); ok {
		return mergeKindMeta(m)
	}
	if d, ok := defaultKindMeta(kind); ok {
		return d
	}
	return engine.DriverMeta{Kind: kind, CountExpr: "COUNT(*)"}
}

func kindLabel(kind string) string {
	return kindMeta(kind).Label
}

// switchConnKind 切换数据源类型；端口还是旧默认值（或空）时跟随新类型的默认端口。
func (a *root) switchConnKind(kind string) {
	old := a.connForm.Kind
	a.connForm.Kind = kind
	port := strings.TrimSpace(a.connForm.Port)
	if port == "" || port == strconv.Itoa(defaultPort(old)) {
		var p int
		if kind == "clickhouse" {
			p = clickhouseProtoPort(a.connProto)
		} else {
			p = defaultPort(kind)
		}
		if p > 0 {
			a.connForm.Port = strconv.Itoa(p)
		} else {
			a.connForm.Port = ""
		}
	}
	a.connForm.Err = ""
	a.connForm.URLErr = ""
	a.resetConnTest()
}

func (a *root) deleteModal(c *ui.Context) {
	name := ""
	for _, p := range a.store.List() {
		if p.ID == a.connForm.Deleting {
			name = p.Name
		}
	}
	ui.Modal(c, &a.connForm.DeleteOpen, func() {
		ui.Text(c, locales.ConnDeleteTitle(name)).Bold()
		ui.Text(c, locales.ConnDeleteHint()).TextColor(c.Theme().TextMuted)
		if m := a.consoleMgr(); m != nil {
			if n := len(m.ConsolesOf(a.connForm.Deleting)); n > 0 {
				ui.Text(c, locales.ConnDeleteConsoles(n)).
					TextColor(c.Theme().Danger)
			}
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.connForm.DeleteOpen = false
			}
			if ui.Button(c, locales.ConnDeleteButton()).Clicked() {
				a.removeConn(a.connForm.Deleting)
				a.connForm.Deleting, a.connForm.DeleteOpen = "", false
			}
		})
	})
}

// applyConnURL 解析连接弹窗顶部的 JDBC/URL 并填入表单；失败只提示，
// 不改动用户已经填好的字段。成功后会清空 URL 输入（通常含密码）。
func (a *root) applyConnURL() {
	raw := strings.TrimSpace(a.connForm.URL)
	if raw == "" {
		a.connForm.URLErr = ""
		return
	}
	p, err := app.ParseConnURL(raw)
	if err != nil {
		if errors.Is(err, app.ErrConnURLUnsupported) {
			a.connForm.URLErr = locales.ConnFormURLUnsupported()
		} else {
			a.connForm.URLErr = locales.ConnFormURLInvalid()
		}
		return
	}
	a.connForm.URLErr = ""
	a.connForm.Err = ""
	a.connForm.Kind = p.Kind
	a.connForm.Host, a.connForm.User, a.connForm.Pass, a.connForm.DB = p.Host, p.User, p.Password, p.Database
	a.connForm.Path = p.Path
	a.connForm.Params = p.Params
	a.connProto = 0
	if p.Kind == "clickhouse" {
		a.connProto = connProtoIndex(p.Protocol)
	}
	a.connForm.Port = ""
	switch {
	case p.Port > 0:
		a.connForm.Port = strconv.Itoa(p.Port)
	case p.Kind == "clickhouse":
		a.connForm.Port = strconv.Itoa(clickhouseProtoPort(a.connProto))
	case p.Kind != "sqlite":
		if port := defaultPort(p.Kind); port > 0 {
			a.connForm.Port = strconv.Itoa(port)
		}
	}
	if p.Kind == "sqlite" {
		a.connForm.Host, a.connForm.User, a.connForm.Pass, a.connForm.DB = "", "", "", ""
		a.connForm.Params = ""
	}
	if strings.TrimSpace(a.connForm.Name) == "" && p.Name != "" {
		a.connForm.Name = p.Name
	}
	a.connForm.ShowPass = false
	a.resetConnTest()
	a.connForm.URL = ""
}

// connFormProfile 把表单当前内容读成一份连接档案（只读，不落盘）：保存、测试连接
// 和复制连接信息都用它，免得三处各读一遍表单。
func (a *root) connFormProfile() (app.ConnProfile, error) {
	p := app.ConnProfile{ID: a.connForm.ID, Name: a.connForm.Name, Kind: a.connForm.Kind, Path: a.connForm.Path, FolderID: a.connForm.Folder}
	sec := 0
	if t := strings.TrimSpace(a.connForm.Timeout); t != "" {
		n, err := strconv.Atoi(t)
		if err != nil {
			return p, errQueryTimeout()
		}
		if n < 0 || n > 86400 {
			return p, errors.New(locales.ConnTimeoutRange())
		}
		sec = n
	}
	p.QueryTimeout = sec
	p.GuardBlock = a.connForm.Guard
	p.AIContextStrict = a.connForm.AIStrict
	p.Tags = a.connTagFormList()
	if a.connForm.Kind == "sqlite" {
		return p, nil
	}
	portText := a.connForm.Port
	if portText == "" {
		portText = strconv.Itoa(defaultPort(a.connForm.Kind))
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return p, errPort()
	}
	p.Host = a.connForm.Host
	p.Port = port
	p.User = a.connForm.User
	p.Password = a.connForm.Pass
	p.Database = a.connForm.DB
	p.Params = strings.TrimSpace(a.connForm.Params)
	if a.connForm.Kind == "clickhouse" {
		p.Protocol = connProtoName(a.connProto)
	}
	return p, nil
}

// connProtoName 把协议下拉的下标换成档案里的协议名。
func connProtoName(idx int) string {
	meta := kindMeta("clickhouse")
	if idx >= 0 && idx < len(meta.Protocols) {
		return meta.Protocols[idx]
	}
	return meta.DefaultProtocol
}

// connProtoIndex 是 connProtoName 的反向：编辑连接时把档案读回下拉。
func connProtoIndex(name string) int {
	meta := kindMeta("clickhouse")
	name = strings.ToLower(name)
	for i, p := range meta.Protocols {
		if p == name {
			return i
		}
	}
	return 0
}

// clickhouseProtoPort 返回 ClickHouse 各协议的默认端口。
func clickhouseProtoPort(idx int) int {
	meta := kindMeta("clickhouse")
	if idx >= 0 && idx < len(meta.Protocols) {
		if p, ok := meta.ProtocolPorts[meta.Protocols[idx]]; ok {
			return p
		}
	}
	return meta.DefaultPort
}

// applyConnProto 换协议后把端口跟着换成该协议的默认端口；用户自己填的端口不动。
func (a *root) applyConnProto() {
	port := strings.TrimSpace(a.connForm.Port)
	old := false
	meta := kindMeta("clickhouse")
	ports := []int{meta.DefaultPort}
	for _, p := range meta.ProtocolPorts {
		ports = append(ports, p)
	}
	for _, p := range ports {
		if port == "" || port == strconv.Itoa(p) {
			old = true
			break
		}
	}
	if old {
		a.connForm.Port = strconv.Itoa(clickhouseProtoPort(a.connProto))
	}
	a.connForm.Err = ""
	a.resetConnTest()
}

// paramsPlaceholder 返回各数据源常用的连接参数示例。
func paramsPlaceholder(kind string) string {
	switch kind {
	case "mysql":
		return locales.ParamsMySQL()
	case "postgres":
		return locales.ParamsPostgres()
	case "clickhouse":
		return locales.ParamsClickHouse()
	}
	return locales.ParamsDefault()
}

func (a *root) saveConnForm() {
	if a.store == nil {
		a.connForm.Err = locales.ConnStoreUnavailable()
		return
	}
	p, err := a.connFormProfile()
	if err != nil {
		a.connForm.Err = err.Error()
		return
	}
	if a.connForm.ID == "" {
		err = a.store.Add(p)
	} else {
		old, had := a.store.Get(a.connForm.ID)
		err = a.store.Update(p)
		if err == nil && had && app.MetaFingerprint(old) != app.MetaFingerprint(p) {
			// 目标库变了：关掉旧会话，旧元数据缓存与内存树都不再适用。
			a.closeSession(p.ID, "connection changed")
			if a.session.Catalog != nil {
				a.session.Catalog.invalidate(p.ID)
			}
			if a.session.Err != nil {
				delete(a.session.Err, p.ID)
			}
			a.forgetConnMeta(p.ID)
		}
	}
	if err != nil {
		a.connForm.Err = err.Error()
		return
	}
	a.connForm.Open = false
	a.resetConnTest()
}

func (a *root) browsePath() {
	if a.headless() {
		return
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  locales.ChooseSQLiteDatabase(),
			Filters: []mygo.FileFilter{
				{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.safeUpdate(win, func() { a.connForm.Path = paths[0] })
	}()
}

// defaultPort 返回各数据源的默认端口。
func defaultPort(kind string) int {
	return kindMeta(kind).DefaultPort
}

// openNewConnForm 重置新建连接表单；folderID 为空表示根级。
func (a *root) openNewConnForm(folderID string) {
	a.connForm.Open, a.connForm.ID, a.connForm.Name, a.connForm.Path, a.connForm.Err = true, "", "", "", ""
	a.connForm.URL, a.connForm.URLErr = "", ""
	a.connForm.Kind, a.connForm.Host, a.connForm.Port = "sqlite", "", ""
	a.connForm.User, a.connForm.Pass, a.connForm.DB = "", "", ""
	a.connForm.ShowPass = false
	a.connForm.Params, a.connForm.Timeout, a.connProto = "", "", 0
	a.connForm.Guard = false
	a.connForm.AIStrict = false
	a.connForm.Folder = folderID
	a.resetConnTagForm()
	a.resetConnTest()
}

// openFolderModal 打开文件夹弹窗；new 用 target 作父文件夹，rename/delete 用 target 作文件夹 ID。
func (a *root) openFolderModal(kind, target, text string) {
	a.modal.FolderKind, a.modal.FolderTarget, a.modal.FolderText, a.modal.FolderErr = kind, target, text, ""
}

func (a *root) folderContentCounts(id string) (conns, folders int) {
	if a.store == nil {
		return 0, 0
	}
	for _, p := range a.store.List() {
		if p.FolderID == id {
			conns++
		}
	}
	for _, f := range a.store.Folders() {
		if f.ParentID == id {
			folders++
		}
	}
	return conns, folders
}

// folderModal 渲染新建/重命名/删除文件夹；数据层负责校验与落盘。
func (a *root) folderModal(c *ui.Context) {
	if a.modal.FolderKind == "" {
		return
	}
	if a.store == nil {
		a.modal.FolderKind = ""
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		switch a.modal.FolderKind {
		case "new":
			title := locales.FolderModalNew()
			if a.modal.FolderTarget != "" {
				title = locales.FolderModalNewSub()
			}
			ui.Text(c, title).Bold()
			ui.TextInput(c, &a.modal.FolderText).Placeholder(locales.FolderNamePlaceholder()).AutoFocus()
		case "rename":
			ui.Text(c, locales.FolderRename()).Bold()
			ui.TextInput(c, &a.modal.FolderText).Placeholder(locales.FolderNamePlaceholder()).AutoFocus()
		case "delete":
			conns, folders := a.folderContentCounts(a.modal.FolderTarget)
			ui.Text(c, locales.FolderDeleteTitle(a.modal.FolderText)).Bold()
			ui.Text(c, locales.FolderDeleteHint(conns, folders)).
				TextColor(c.Theme().TextMuted)
		}
		if a.modal.FolderErr != "" {
			ui.Text(c, a.modal.FolderErr).TextColor(c.Theme().Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.modal.FolderKind = ""
			}
			if a.modal.FolderKind == "delete" {
				btn := ui.Button(c, locales.ConnDeleteButton()).TextColor(c.Theme().Danger)
				if btn.Clicked() {
					if err := a.store.DeleteFolder(a.modal.FolderTarget); err != nil {
						a.modal.FolderErr = locales.ModalDeleteFailed(err.Error())
						return
					}
					delete(a.tree.FolderCollapsed, a.modal.FolderTarget)
					app.LogInfo("conn", "folder deleted", "id", a.modal.FolderTarget)
					a.modal.FolderKind = ""
				}
				return
			}
			if ui.PrimaryButton(c, locales.CommonSave()).Clicked() {
				name := strings.TrimSpace(a.modal.FolderText)
				if name == "" {
					a.modal.FolderErr = locales.ModalNameEmpty()
					return
				}
				switch a.modal.FolderKind {
				case "new":
					f, err := a.store.AddFolder(name, a.modal.FolderTarget)
					if err != nil {
						a.modal.FolderErr = locales.FolderCreateFailed(err.Error())
						return
					}
					delete(a.tree.FolderCollapsed, a.modal.FolderTarget) // 父级展开，新文件夹可见
					app.LogInfo("conn", "folder added", "id", f.ID, "name", f.Name, "parent", f.ParentID)
				case "rename":
					if err := a.store.RenameFolder(a.modal.FolderTarget, name); err != nil {
						a.modal.FolderErr = locales.ModalRenameFailed(err.Error())
						return
					}
					app.LogInfo("conn", "folder renamed", "id", a.modal.FolderTarget, "name", name)
				}
				a.modal.FolderKind = ""
			}
		})
	})
	if !open {
		a.modal.FolderKind = ""
	}
}

// folderChoice 是连接弹窗「位置」下拉的一项：ID + 全路径标签。
type folderChoice struct{ id, label string }

func folderOptions(folders []app.ConnFolder) []folderChoice {
	byParent := map[string][]app.ConnFolder{}
	for _, f := range folders {
		byParent[f.ParentID] = append(byParent[f.ParentID], f)
	}
	var out []folderChoice
	seen := map[string]bool{}
	var walk func(parent string)
	walk = func(parent string) {
		for _, f := range byParent[parent] {
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			label := app.FolderPath(folders, f.ID)
			if label == "" {
				label = f.Name
			}
			out = append(out, folderChoice{id: f.ID, label: label})
			walk(f.ID)
		}
	}
	walk("")
	return out
}

func (a *root) folderLabel(id string) string {
	if id == "" || a.store == nil {
		return locales.FolderRoot()
	}
	if path := app.FolderPath(a.store.Folders(), id); path != "" {
		return path
	}
	return locales.FolderRoot()
}

// folderSelect 是连接弹窗里的文件夹选择器：直接绑定 folder ID，避免标签映射。
func (a *root) folderSelect(c *ui.Context) {
	t := c.Theme()
	sel := ui.SelectBase(c, &a.connForm.Folder)
	sel.Trigger.Gap(6).Padding(6, 10).Radius(t.Space(2)).Border(1, t.Border).
		Children(func() {
			ui.Text(c, a.folderLabel(a.connForm.Folder)).SingleLine()
			ui.Icon(c, iconChevronDown).FontSize(14).TextColor(t.TextMuted)
		})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(t.Space(1)).Background(t.Background).Border(1, t.Border).Width(260)
		item := func(id, label string) {
			it := sel.Item(id).Padding(6, 10).Radius(t.Space(0.5))
			if it.Highlighted() {
				it.Background(t.Accent.Alpha(0.12))
			}
			it.Children(func() { ui.Text(c, label).SingleLine() })
		}
		item("", locales.FolderRoot())
		if a.store != nil {
			for _, o := range folderOptions(a.store.Folders()) {
				item(o.id, o.label)
			}
		}
	})
}
