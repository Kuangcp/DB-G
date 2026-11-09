package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func (a *root) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		ui.Toolbar(c, func() {
			if iconButton(c, iconPlug, "新建连接", ui.Transparent).Clicked() {
				a.openNewConnForm("")
			}
			if iconButton(c, iconFolderPlus, "新建文件夹", ui.Transparent).Clicked() {
				a.openFolderModal("new", "", "")
			}
			if iconButton(c, iconRefresh, "重载对象树（不连接数据源）", ui.Transparent).Clicked() {
				a.reloadTree()
			}
			ui.Spacer(c)
		}).Label("数据源操作")
		if a.store == nil {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
			a.connModal(c)
			a.folderModal(c)
			return
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c, &a.objSearch).Placeholder("搜索表/视图…").Label("搜索对象").Fill()
			if a.objSearch != "" {
				if iconButtonPlain(c, iconClose, "清除搜索", ui.Transparent).Clicked() {
					a.objSearch = ""
				}
			}
		})
		if a.objSearchActive() {
			ui.Text(c, "搜索仅覆盖已加载的对象").FontSize(11.5).TextColor(c.Theme().TextMuted)
		}
		if w := a.store.Warning(); w != "" {
			ui.Text(c, w).TextColor(c.Theme().Danger)
		}
		if len(a.store.List()) == 0 && len(a.store.Folders()) == 0 {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
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
func (a *root) objSearchActive() bool { return strings.TrimSpace(a.objSearch) != "" }

// objMatch 报告名字是否命中当前搜索词（大小写不敏感）。
func (a *root) objMatch(name string) bool {
	q := strings.ToLower(strings.TrimSpace(a.objSearch))
	return q != "" && strings.Contains(strings.ToLower(name), q)
}

// connObjectMatches 报告连接已加载的对象里是否有命中搜索词的对象。
func (a *root) connObjectMatches(connID string) bool {
	for base, m := range a.treeData {
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
	for _, objs := range a.treeData[base] {
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
		row.Cursor(ui.CursorGrab).Tooltip("按住 Ctrl 拖动：上下排序，拖到文件夹中间移入")
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
		delete(a.folderCollapsed, targetID) // 移入后展开，结果立即可见
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
	collapsed := a.folderCollapsed[f.ID] && !a.objSearchActive()
	row := treeRowBase(c, "文件夹 "+f.Name).PaddingStart(float32(4 + depth*folderIndent))
	row.Children(func() {
		var label string
		if collapsed {
			label = "展开 " + f.Name
		} else {
			label = "收起 " + f.Name
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
		if menu.Item("新建子文件夹…").Chosen() {
			a.openFolderModal("new", f.ID, "")
		}
		if menu.Item("在此新建连接…").Chosen() {
			a.openNewConnForm(f.ID)
		}
		if menu.Item("重命名…").Chosen() {
			a.openFolderModal("rename", f.ID, f.Name)
		}
		if menu.Item("删除…").Chosen() {
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
	if a.folderCollapsed == nil {
		a.folderCollapsed = map[string]bool{}
	}
	a.folderCollapsed[id] = !a.folderCollapsed[id]
}

func (a *root) connRow(c *ui.Context, p app.ConnProfile, depth int, idx *connTreeIndex) {
	live := a.connected(p.ID)
	expanded := a.treeExpand[p.ID] || a.objSearchActive()
	row := treeRowBase(c, "连接 "+p.Name).PaddingStart(float32(4 + depth*folderIndent))
	row.Children(func() {
		var label string
		if expanded {
			label = "收起 " + p.Name
		} else {
			label = "展开 " + p.Name
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
		if iconButtonPlain(c, iconSquarePen, "编辑连接", ui.Transparent).Clicked() {
			a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, p.ID, p.Name, p.Path, ""
			a.connFormKind = p.Kind
			a.connFormHost, a.connFormUser, a.connFormPass, a.connFormDB = p.Host, p.User, p.Password, p.Database
			a.connFormPort = ""
			if p.Port > 0 {
				a.connFormPort = strconv.Itoa(p.Port)
			}
			a.connFormParams = p.Params
			a.connFormTimeout = ""
			if p.QueryTimeout > 0 {
				a.connFormTimeout = strconv.Itoa(p.QueryTimeout)
			}
			a.connFormGuard = p.GuardBlock
			a.connProto = connProtoIndex(p.Protocol)
			a.connFormFolder = p.FolderID
			a.setConnTagForm(p.Tags)
			a.resetConnTest()
		}
	})
	// 复制数据源 / 删除连接是低频且易误触的操作：放右键菜单，不占行内位置。
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item("复制数据源").Chosen() {
			a.duplicateConn(p)
		}
		menu.Separator()
		if menu.Item("删除连接…").Chosen() {
			a.connDeleting, a.connDeleteOpen = p.ID, true
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
			a.objectGroups(c, p)
		})
	}
}

// duplicateConn 在同文件夹下复制一份数据源：名称自动取未被占用的「副本：X」，
// 只落盘一次（Add 内部生成新 ID）。
func (a *root) duplicateConn(p app.ConnProfile) {
	if a.store == nil {
		a.connErr = "复制失败：连接库不可用"
		return
	}
	taken := make(map[string]bool)
	for _, q := range a.store.List() {
		taken[q.Name] = true
	}
	name := "副本：" + p.Name
	for i := 1; taken[name]; i++ {
		name = fmt.Sprintf("%s (副本%d)", p.Name, i)
	}
	cloned := app.ConnProfile{
		Name:         name,
		Kind:         p.Kind,
		Path:         p.Path,
		Host:         p.Host,
		Port:         p.Port,
		User:         p.User,
		Password:     p.Password,
		Database:     p.Database,
		Protocol:     p.Protocol,
		Params:       p.Params,
		FolderID:     p.FolderID,
		QueryTimeout: p.QueryTimeout,
		Tags:         p.Tags,
	}
	if err := a.store.Add(cloned); err != nil {
		a.connErr = "复制失败：" + err.Error()
		return
	}
	app.LogInfo("conn", "duplicate", "from", p.Name, "to", name, "folder", p.FolderID)
}

// consoleSection 列出该数据源的全部控制台（打开与否两种状态）并提供新建入口。
func (a *root) consoleSection(c *ui.Context, p app.ConnProfile) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "控制台").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		if iconButtonPlain(c, iconPlus, "新建控制台", ui.Transparent).Clicked() {
			rec, err := m.Create(p.ID, "")
			if err != nil {
				a.consoleErr = "新建控制台失败: " + err.Error()
				return
			}
			a.pendingActivate = rec.ID
		}
	})
	for _, rec := range m.ConsolesOf(p.ID) {
		a.consoleRow(c, rec, m.IsOpen(rec.ID))
	}
}

// consoleRow 渲染一个控制台：已打开加粗，点击打开或激活，右键关闭/重命名/删除。
func (a *root) consoleRow(c *ui.Context, rec app.ConsoleRecord, isOpen bool) {
	row := treeRowBase(c, "控制台 "+rec.Name)
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
		a.pendingActivate = rec.ID
	}
	row.ContextMenu(func(menu *ui.Menu) {
		if menu.Item("关闭").Disabled(!isOpen).Chosen() {
			a.pendingClose = rec.ID
		}
		if menu.Item("重命名…").Chosen() {
			a.openModal("rename", rec.ID, rec.Name)
		}
		if menu.Item("删除…").Chosen() {
			a.openModal("delete", rec.ID, rec.Name)
		}
	})
}

// 连接弹窗的固定尺寸：切换数据源时表单行数不同，面板却不变，窗口不会一弹一弹。
const (
	connFormWidth  = 640
	connFormHeight = 660
)

func (a *root) connModal(c *ui.Context) {
	if a.connDeleteOpen {
		a.deleteModal(c)
		return
	}
	ui.Modal(c, &a.connFormOpen, func() {
		radius := controlRadius(c)
		title := "新建连接"
		if a.connFormID != "" {
			title = "编辑连接"
		}
		ui.Text(c, title).Bold()
		// 左右布局：左侧数据源列表（无按钮外观、行间分隔线、选中高亮），右侧表单。
		ui.Row(c).Fill().Gap(0).Children(func() {
			ui.Column(c).Width(168).FillHeight().Children(func() {
				for _, k := range connKinds {
					a.connKindRow(c, k)
				}
				ui.Spacer(c)
			})
			ui.Box(c).Width(1).FillHeight().Background(c.Theme().Border)
			ui.Column(c).Fill().Padding(0, 16).Gap(8).Children(func() {
				ui.Text(c, kindLabel(a.connFormKind)+" 连接").Bold()
				ui.TextInput(c, &a.connFormName).Placeholder("名称").Radius(radius).AutoFocus()
				if a.store != nil && len(a.store.Folders()) > 0 {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "位置").FontSize(12).TextColor(c.Theme().TextMuted)
						a.folderSelect(c)
						ui.Spacer(c)
					})
				}
				if a.connFormKind != "sqlite" {
					ui.TextInput(c, &a.connFormDB).Placeholder("数据库（可选）").Radius(radius)
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connFormHost).Placeholder("主机").Radius(radius).Fill()
						ui.TextInput(c, &a.connFormPort).Placeholder("端口").Radius(radius)
					})
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connFormUser).Placeholder("用户").Radius(radius).Fill()
						ui.TextInput(c, &a.connFormPass).Placeholder("密码").Radius(radius).Password()
					})
					if a.connFormKind == "clickhouse" {
						// 协议决定 DSN 的 scheme，也决定默认端口：native 9000、
						// HTTP 8123、HTTPS 8443。填错端口会握手失败。
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "协议").FontSize(12).TextColor(c.Theme().TextMuted)
							if ui.Segmented(c, &a.connProto, "Native", "HTTP", "HTTPS").Radius(radius).Changed() {
								a.applyConnProto()
							}
							ui.Spacer(c)
						})
					}
					ui.TextInput(c, &a.connFormParams).Placeholder(paramsPlaceholder(a.connFormKind)).Radius(radius)
				} else {
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connFormPath).Placeholder("SQLite 文件路径").Radius(radius).Fill()
						if ui.Button(c, "浏览…").Radius(radius).Clicked() {
							a.browsePath()
						}
					})
				}
				ui.TextInput(c, &a.connFormTimeout).
					Placeholder("查询超时（秒，空或 0 = 默认 30）").Radius(radius)
				ui.Text(c, "标签（最多 3 个）").FontSize(12).TextColor(c.Theme().TextMuted)
				for i := range a.connFormTags {
					tag := &a.connFormTags[i]
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						swatch := ui.Button(c, "●").FontSize(13).TextColor(tagColor(tag.color)).
							Radius(radius).Padding(2, 8)
						swatch.Tooltip("标签颜色：" + tagColorLabel(tag.color) + "（点击切换）")
						if swatch.Clicked() {
							tag.color = nextTagColor(tag.color)
						}
						ui.TextInput(c, &tag.label).
							Placeholder(fmt.Sprintf("标签 %d（可选）", i+1)).
							Radius(radius).Fill()
					})
				}
				ui.Checkbox(c, &a.connFormGuard, "高危 SQL 直接阻断（默认只提醒）")

				if a.connFormErr != "" {
					ui.Text(c, a.connFormErr).TextColor(c.Theme().Danger)
				}
				// 留白：按钮沉到面板底部，上方空着。
				ui.Spacer(c)
				if a.connTestMsg != "" {
					col := c.Theme().Success
					if a.connTestErr {
						col = c.Theme().Danger
					}
					ui.Text(c, a.connTestMsg).FontSize(12).TextColor(col).
						SingleLine().Tooltip(a.connTestMsg)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if a.connTestBusy {
						if ui.Button(c, "取消测试").Radius(radius).Clicked() {
							a.cancelConnTest()
						}
						ui.Spacer(c)
						return
					}
					if ui.Button(c, "测试连接").Radius(radius).Clicked() {
						a.testConnForm()
					}
					if ui.Button(c, "复制").Radius(radius).Tooltip("把连接信息按 key: value 复制到剪贴板（含明文密码）").
						Clicked() {
						a.copyConnForm(c)
					}
					ui.Spacer(c)
					if ui.Button(c, "取消").Radius(radius).Clicked() {
						a.connFormOpen = false
					}
					if ui.PrimaryButton(c, "保存").Radius(radius).Clicked() {
						a.saveConnForm()
					}
				})
			})
		})
	}).Width(connFormWidth).Height(connFormHeight)
}

// connKindRow 是左侧数据源类型的一行：走通用的侧边导航样式（无按钮外观、
// 行底分隔线、选中高亮）。
func (a *root) connKindRow(c *ui.Context, k connKind) {
	selected := a.connFormKind == k.kind
	row := sideNavRow(c, k.label, connKindIcon(k.kind), connKindColor(k.kind), selected)
	if row.Clicked() {
		a.switchConnKind(k.kind)
	}
}

// connKind 是连接类型导航项。
type connKind struct{ kind, label string }

var connKinds = []connKind{
	{"sqlite", "SQLite"},
	{"mysql", "MySQL"},
	{"postgres", "PostgreSQL"},
	{"clickhouse", "ClickHouse"},
}

func kindLabel(kind string) string {
	for _, k := range connKinds {
		if k.kind == kind {
			return k.label
		}
	}
	return kind
}

// switchConnKind 切换数据源类型；端口还是旧默认值（或空）时跟随新类型的默认端口。
func (a *root) switchConnKind(kind string) {
	old := a.connFormKind
	a.connFormKind = kind
	port := strings.TrimSpace(a.connFormPort)
	if port == "" || port == strconv.Itoa(defaultPort(old)) {
		var p int
		if kind == "clickhouse" {
			p = clickhouseProtoPort(a.connProto)
		} else {
			p = defaultPort(kind)
		}
		if p > 0 {
			a.connFormPort = strconv.Itoa(p)
		} else {
			a.connFormPort = ""
		}
	}
	a.connFormErr = ""
	a.resetConnTest()
}

func (a *root) deleteModal(c *ui.Context) {
	name := ""
	for _, p := range a.store.List() {
		if p.ID == a.connDeleting {
			name = p.Name
		}
	}
	ui.Modal(c, &a.connDeleteOpen, func() {
		ui.Text(c, "删除连接「"+name+"」？").Bold()
		ui.Text(c, "仅移除配置，不删除数据库文件").TextColor(c.Theme().TextMuted)
		if m := a.consoleMgr(); m != nil {
			if n := len(m.ConsolesOf(a.connDeleting)); n > 0 {
				ui.Text(c, fmt.Sprintf("将同时删除该数据源的 %d 个控制台及其 .sql 文件", n)).
					TextColor(c.Theme().Danger)
			}
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.connDeleteOpen = false
			}
			if ui.Button(c, "删除").Clicked() {
				a.removeConn(a.connDeleting)
				a.connDeleting, a.connDeleteOpen = "", false
			}
		})
	})
}

// connFormProfile 把表单当前内容读成一份连接档案（只读，不落盘）：保存、测试连接
// 和复制连接信息都用它，免得三处各读一遍表单。
func (a *root) connFormProfile() (app.ConnProfile, error) {
	p := app.ConnProfile{ID: a.connFormID, Name: a.connFormName, Kind: a.connFormKind, Path: a.connFormPath, FolderID: a.connFormFolder}
	sec := 0
	if t := strings.TrimSpace(a.connFormTimeout); t != "" {
		n, err := strconv.Atoi(t)
		if err != nil {
			return p, errQueryTimeout
		}
		if n < 0 || n > 86400 {
			return p, errors.New("查询超时需在 0–86400 秒")
		}
		sec = n
	}
	p.QueryTimeout = sec
	p.GuardBlock = a.connFormGuard
	p.Tags = a.connTagFormList()
	if a.connFormKind == "sqlite" {
		return p, nil
	}
	portText := a.connFormPort
	if portText == "" {
		portText = strconv.Itoa(defaultPort(a.connFormKind))
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return p, errPort
	}
	p.Host = a.connFormHost
	p.Port = port
	p.User = a.connFormUser
	p.Password = a.connFormPass
	p.Database = a.connFormDB
	p.Params = strings.TrimSpace(a.connFormParams)
	if a.connFormKind == "clickhouse" {
		p.Protocol = connProtoName(a.connProto)
	}
	return p, nil
}

// connProtoName 把协议下拉的下标换成档案里的协议名。
func connProtoName(idx int) string {
	switch idx {
	case 1:
		return "http"
	case 2:
		return "https"
	}
	return "native"
}

// connProtoIndex 是 connProtoName 的反向：编辑连接时把档案读回下拉。
func connProtoIndex(name string) int {
	switch strings.ToLower(name) {
	case "http":
		return 1
	case "https":
		return 2
	}
	return 0
}

// clickhouseProtoPort 返回 ClickHouse 各协议的默认端口。
func clickhouseProtoPort(idx int) int {
	switch idx {
	case 1:
		return 8123
	case 2:
		return 8443
	}
	return 9000
}

// applyConnProto 换协议后把端口跟着换成该协议的默认端口；用户自己填的端口不动。
func (a *root) applyConnProto() {
	port := strings.TrimSpace(a.connFormPort)
	old := false
	for _, p := range []int{9000, 8123, 8443} {
		if port == "" || port == strconv.Itoa(p) {
			old = true
			break
		}
	}
	if old {
		a.connFormPort = strconv.Itoa(clickhouseProtoPort(a.connProto))
	}
	a.connFormErr = ""
	a.resetConnTest()
}

// paramsPlaceholder 返回各数据源常用的连接参数示例。
func paramsPlaceholder(kind string) string {
	switch kind {
	case "mysql":
		return "连接参数（可选）：tls=true&parseTime=true"
	case "postgres":
		return "连接参数（可选）：sslmode=require"
	case "clickhouse":
		return "连接参数（可选）：compress=0&secure=false"
	}
	return "连接参数（可选）：key=value"
}

func (a *root) saveConnForm() {
	if a.store == nil {
		a.connFormErr = "连接库不可用"
		return
	}
	p, err := a.connFormProfile()
	if err != nil {
		a.connFormErr = err.Error()
		return
	}
	if a.connFormID == "" {
		err = a.store.Add(p)
	} else {
		old, had := a.store.Get(a.connFormID)
		err = a.store.Update(p)
		if err == nil && had && app.MetaFingerprint(old) != app.MetaFingerprint(p) {
			// 目标库变了：关掉旧会话，旧元数据缓存与内存树都不再适用。
			a.closeSession(p.ID, "connection changed")
			if a.catalog != nil {
				a.catalog.invalidate(p.ID)
			}
			if a.sessionErr != nil {
				delete(a.sessionErr, p.ID)
			}
			a.forgetConnMeta(p.ID)
		}
	}
	if err != nil {
		a.connFormErr = err.Error()
		return
	}
	a.connFormOpen = false
	a.resetConnTest()
}

func (a *root) browsePath() {
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  "选择 SQLite 数据库",
			Filters: []mygo.FileFilter{
				{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.safeUpdate(win, func() { a.connFormPath = paths[0] })
	}()
}

// defaultPort 返回各数据源的默认端口。
func defaultPort(kind string) int {
	switch kind {
	case "mysql":
		return 3306
	case "postgres":
		return 5432
	case "clickhouse":
		return 9000
	}
	return 0
}

// openNewConnForm 重置新建连接表单；folderID 为空表示根级。
func (a *root) openNewConnForm(folderID string) {
	a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, "", "", "", ""
	a.connFormKind, a.connFormHost, a.connFormPort = "sqlite", "", ""
	a.connFormUser, a.connFormPass, a.connFormDB = "", "", ""
	a.connFormParams, a.connFormTimeout, a.connProto = "", "", 0
	a.connFormGuard = false
	a.connFormFolder = folderID
	a.resetConnTagForm()
	a.resetConnTest()
}

// openFolderModal 打开文件夹弹窗；new 用 target 作父文件夹，rename/delete 用 target 作文件夹 ID。
func (a *root) openFolderModal(kind, target, text string) {
	a.folderModalKind, a.folderModalTarget, a.folderModalText, a.folderModalErr = kind, target, text, ""
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
	if a.folderModalKind == "" {
		return
	}
	if a.store == nil {
		a.folderModalKind = ""
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		switch a.folderModalKind {
		case "new":
			title := "新建文件夹"
			if a.folderModalTarget != "" {
				title = "新建子文件夹"
			}
			ui.Text(c, title).Bold()
			ui.TextInput(c, &a.folderModalText).Placeholder("文件夹名称").AutoFocus()
		case "rename":
			ui.Text(c, "重命名文件夹").Bold()
			ui.TextInput(c, &a.folderModalText).Placeholder("文件夹名称").AutoFocus()
		case "delete":
			conns, folders := a.folderContentCounts(a.folderModalTarget)
			ui.Text(c, "删除文件夹「"+a.folderModalText+"」？").Bold()
			ui.Text(c, fmt.Sprintf("其中 %d 个连接和 %d 个子文件夹将移到上一级；连接配置不会删除。", conns, folders)).
				TextColor(c.Theme().TextMuted)
		}
		if a.folderModalErr != "" {
			ui.Text(c, a.folderModalErr).TextColor(c.Theme().Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.folderModalKind = ""
			}
			if a.folderModalKind == "delete" {
				btn := ui.Button(c, "删除").TextColor(c.Theme().Danger)
				if btn.Clicked() {
					if err := a.store.DeleteFolder(a.folderModalTarget); err != nil {
						a.folderModalErr = "删除失败: " + err.Error()
						return
					}
					delete(a.folderCollapsed, a.folderModalTarget)
					app.LogInfo("conn", "folder deleted", "id", a.folderModalTarget)
					a.folderModalKind = ""
				}
				return
			}
			if ui.PrimaryButton(c, "保存").Clicked() {
				name := strings.TrimSpace(a.folderModalText)
				if name == "" {
					a.folderModalErr = "名称不能为空"
					return
				}
				switch a.folderModalKind {
				case "new":
					f, err := a.store.AddFolder(name, a.folderModalTarget)
					if err != nil {
						a.folderModalErr = "新建失败: " + err.Error()
						return
					}
					delete(a.folderCollapsed, a.folderModalTarget) // 父级展开，新文件夹可见
					app.LogInfo("conn", "folder added", "id", f.ID, "name", f.Name, "parent", f.ParentID)
				case "rename":
					if err := a.store.RenameFolder(a.folderModalTarget, name); err != nil {
						a.folderModalErr = "重命名失败: " + err.Error()
						return
					}
					app.LogInfo("conn", "folder renamed", "id", a.folderModalTarget, "name", name)
				}
				a.folderModalKind = ""
			}
		})
	})
	if !open {
		a.folderModalKind = ""
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
		return "根目录"
	}
	if path := app.FolderPath(a.store.Folders(), id); path != "" {
		return path
	}
	return "根目录"
}

// folderSelect 是连接弹窗里的文件夹选择器：直接绑定 folder ID，避免标签映射。
func (a *root) folderSelect(c *ui.Context) {
	t := c.Theme()
	sel := ui.SelectBase(c, &a.connFormFolder)
	sel.Trigger.Gap(6).Padding(6, 10).Radius(t.Space(2)).Border(1, t.Border).
		Children(func() {
			ui.Text(c, a.folderLabel(a.connFormFolder)).SingleLine()
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
		item("", "根目录")
		if a.store != nil {
			for _, o := range folderOptions(a.store.Folders()) {
				item(o.id, o.label)
			}
		}
	})
}
