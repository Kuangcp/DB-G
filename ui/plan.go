package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/ai/plan"
	"db-g/app"
	"db-g/engine"
)

// planState 是执行计划结果区的状态。
type planState struct {
	open    bool
	loading bool
	confirm bool
	analyze bool
	sql     string
	kind    string
	err     string
	raw     string
	nodes   []plan.PlanNode
}

// planTimeout 限住一次 EXPLAIN；ANALYZE 可能跑得久一些。
const planTimeout = 20 * time.Second

// explainSQL 返回当前要分析的 SQL：选区优先，否则光标语句。
func (a *root) explainSQL() (string, bool) {
	if sel, ok := a.code.SelectedText(); ok && strings.TrimSpace(sel) != "" {
		return strings.TrimSpace(sel), true
	}
	text := a.code.Text()
	if st, ok := app.StatementAt(text, a.code.state.Cursor()); ok {
		sql := strings.TrimSpace(text[st.Start:st.End])
		return sql, sql != ""
	}
	return "", false
}

// runExplain 生成执行计划；analyze 为 true 时必须由调用方先确认。
func (a *root) runExplain(analyze bool) {
	if a.plan.loading {
		return
	}
	sql, ok := a.explainSQL()
	if !ok {
		a.consoleErr = "没有可分析的 SQL"
		return
	}
	if analyze {
		switch strings.ToUpper(firstWord(sql)) {
		case "SELECT", "WITH":
		default:
			a.consoleErr = "EXPLAIN ANALYZE 只允许 SELECT/WITH"
			return
		}
	}
	sess := a.activeSession()
	if sess == nil {
		a.consoleErr = "请先连接数据源"
		return
	}
	if analyze && !sess.Caps().Has(engine.CapExplainAnalyze) {
		a.consoleErr = "当前数据源不支持 EXPLAIN ANALYZE"
		return
	}
	pe, ok := sess.(engine.PlanExplainer)
	if !ok || !sess.Caps().Has(engine.CapExplain) {
		a.consoleErr = "当前数据源不支持执行计划"
		return
	}
	kind := ""
	if p, ok := a.store.Get(a.activeConnID()); ok {
		kind = p.Kind
	}
	a.plan = planState{
		open: true, loading: true, analyze: analyze, sql: sql, kind: kind,
	}
	run := func() (*engine.QueryResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), planTimeout)
		defer cancel()
		return pe.Explain(ctx, sql, engine.ExplainOptions{Analyze: analyze, Buffers: analyze})
	}
	done := func(res *engine.QueryResult, err error) {
		a.plan.loading = false
		if err != nil {
			a.plan.err = err.Error()
			return
		}
		a.plan.raw = planRaw(res)
		nodes, perr := plan.Parse(kind, res.Columns, res.Rows)
		if perr != nil {
			a.plan.err = perr.Error()
			return
		}
		a.plan.nodes = nodes
		app.LogInfo("ai", "plan parsed", "kind", kind, "nodes", countPlanNodes(nodes), "analyze", analyze)
	}
	if a.win == nil {
		done(run())
		return
	}
	win := a.win
	go func() {
		res, err := run()
		a.safeUpdate(win, func() { done(res, err) })
	}()
}

func countPlanNodes(nodes []plan.PlanNode) int {
	n := len(nodes)
	for _, node := range nodes {
		n += countPlanNodes(node.Children)
	}
	return n
}

func planRaw(res *engine.QueryResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, row := range res.Rows {
		for i, v := range row {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(fmt.Sprint(v))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// planPane 在结果区展示执行计划；它覆盖普通结果，关闭后回到结果表。
func (a *root) planPane(c *ui.Context) {
	t := c.Theme()
	ps := &a.plan
	ui.Column(c).Fill().Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "执行计划").Bold()
			if ps.kind != "" {
				ui.Badge(c, ps.kind)
			}
			if ps.analyze {
				ui.Badge(c, "ANALYZE")
			}
			ui.Spacer(c)
			if !ps.loading && len(ps.nodes) > 0 {
				if ui.Button(c, "AI 诊断").Clicked() {
					a.diagnosePlan()
				}
				if a.planAnalyzeSupported() && ui.Button(c, "ANALYZE 重跑").Clicked() {
					ps.confirm = true
				}
			}
			if ui.Button(c, "关闭").Clicked() {
				ps.open = false
			}
		})
		switch {
		case ps.loading:
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "分析中…")
			})
		case ps.err != "":
			ui.Text(c, "计划解析失败").Bold().TextColor(t.Danger)
			a.withResultFont(ui.Text(c, ps.err).TextColor(t.Danger).Font("monospace"))
		}
		if len(ps.nodes) > 0 {
			maxCost := planMaxCost(ps.nodes)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					for _, node := range ps.nodes {
						a.planNode(c, node, 0, maxCost)
					}
				})
			})
		} else if ps.raw != "" && !ps.loading {
			ui.Text(c, "原始计划").FontSize(11).TextColor(t.TextMuted)
			a.withResultFont(ui.Text(c, ps.raw).Font("monospace"))
		}
	})
}

// planAnalyzeSupported 报告当前数据源是否支持 EXPLAIN ANALYZE。
func (a *root) planAnalyzeSupported() bool {
	sess := a.activeSession()
	return sess != nil && sess.Caps().Has(engine.CapExplainAnalyze)
}

func planMaxCost(nodes []plan.PlanNode) float64 {
	max := 0.0
	for _, n := range nodes {
		if n.Cost > max {
			max = n.Cost
		}
		if c := planMaxCost(n.Children); c > max {
			max = c
		}
	}
	return max
}

func (a *root) planNode(c *ui.Context, n plan.PlanNode, depth int, maxCost float64) {
	t := c.Theme()
	col := t.Text
	if maxCost > 0 && n.Cost >= maxCost*0.5 {
		col = t.Danger
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(0, float32(depth)*14).Children(func() {
		ui.Text(c, "•").FontSize(10).TextColor(t.TextMuted)
		ui.Text(c, n.Op).FontSize(12).Bold().TextColor(col)
		if n.Relation != "" {
			ui.Text(c, n.Relation).FontSize(12).TextColor(t.TextMuted)
		}
		if n.StartupCost > 0 && n.Cost > 0 {
			ui.Text(c, fmt.Sprintf("startup %.1f → cost %.1f", n.StartupCost, n.Cost)).FontSize(11).TextColor(t.TextMuted)
		} else if n.Cost > 0 {
			ui.Text(c, fmt.Sprintf("cost %.1f", n.Cost)).FontSize(11).TextColor(t.TextMuted)
		}
		if n.Rows > 0 {
			ui.Text(c, fmt.Sprintf("rows %.0f", n.Rows)).FontSize(11).TextColor(t.TextMuted)
		}
		if n.Actual > 0 {
			ui.Text(c, "actual "+n.Actual.String()).FontSize(11).TextColor(t.TextMuted)
		}
		if n.ActualRows > 0 {
			ui.Text(c, fmt.Sprintf("actual rows %.0f", n.ActualRows)).FontSize(11).TextColor(t.TextMuted)
		}
		if n.Loops > 1 {
			ui.Text(c, fmt.Sprintf("loops %.0f", n.Loops)).FontSize(11).TextColor(t.TextMuted)
		}
		if n.Buffers != "" {
			ui.Text(c, n.Buffers).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
		}
		if n.Detail != "" && n.Detail != n.Op {
			ui.Text(c, n.Detail).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
		}
	})
	for _, child := range n.Children {
		a.planNode(c, child, depth+1, maxCost)
	}
}

// planConfirmModal 是 EXPLAIN ANALYZE 的每次确认。
func (a *root) planConfirmModal(c *ui.Context) {
	if !a.plan.confirm {
		return
	}
	ui.Modal(c, &a.plan.confirm, func() {
		ui.Text(c, "执行 EXPLAIN ANALYZE？").Bold()
		ui.Text(c, "ANALYZE 会真实执行当前 SELECT，可能较慢；只对 SELECT/WITH 开放。").
			FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, a.plan.sql).Font("monospace").FontSize(11).SingleLine().Tooltip(a.plan.sql)
		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.plan.confirm = false
			}
			if ui.PrimaryButton(c, "执行 ANALYZE").Clicked() {
				a.plan.confirm = false
				a.runExplain(true)
			}
		})
	}).Width(560).Height(240)
}

// diagnosePlan 把计划树发给 AI 面板诊断。
func (a *root) diagnosePlan() {
	if len(a.plan.nodes) == 0 {
		return
	}
	snap := a.aiSnapshot()
	msgs := ai.BuildPlanMessages(snap, plan.Text(a.plan.nodes), a.plan.analyze)
	a.aiChat.open = true
	a.aiChat.lastCtx = "上下文：当前连接 + 执行计划"
	if err := a.startAIRequest("诊断执行计划", msgs); err != nil {
		a.aiChat.err = err.Error()
	}
}
