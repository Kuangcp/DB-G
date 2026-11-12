// Package plan 把各数据源的 EXPLAIN 输出解析成统一计划树。
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PlanNode 是一个执行计划节点。
type PlanNode struct {
	Op          string
	Relation    string
	StartupCost float64
	Cost        float64
	Rows        float64
	Actual      time.Duration // 实际耗时（PostgreSQL 为每 loop 平均，MySQL TREE 为实际起点）
	ActualRows  float64
	Loops       float64
	Buffers     string // PostgreSQL BUFFERS 统计，已格式化为一行
	Detail      string
	Children    []PlanNode
}

// Parse 按数据源 kind 解析 EXPLAIN 结果。
func Parse(kind string, columns []string, rows [][]any) ([]PlanNode, error) {
	if len(rows) == 0 {
		return nil, errors.New("执行计划为空")
	}
	switch strings.ToLower(kind) {
	case "sqlite":
		return parseSQLite(columns, rows)
	case "postgres":
		return parsePostgres(cell(rows, 0, 0))
	case "mysql":
		raw := cell(rows, 0, 0)
		if strings.Contains(raw, "->") || (len(columns) == 1 && len(rows) > 1) {
			if nodes := parseTree(raw); len(nodes) > 0 {
				return nodes, nil
			}
		}
		return parseMySQL(raw)
	case "clickhouse":
		return parseClickHouse(cell(rows, 0, 0))
	default:
		if nodes := parseTree(rawText(columns, rows)); len(nodes) > 0 {
			return nodes, nil
		}
	}
	return nil, fmt.Errorf("无法解析 %s 的执行计划", kind)
}

func cell(rows [][]any, r, c int) string {
	if r < 0 || r >= len(rows) || c < 0 || c >= len(rows[r]) {
		return ""
	}
	switch v := rows[r][c].(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func rawText(columns []string, rows [][]any) string {
	var b strings.Builder
	for _, row := range rows {
		for i, v := range row {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(fmt.Sprint(v))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	}
	return 0
}

func text(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	default:
		return fmt.Sprint(s)
	}
}

// ---- PostgreSQL ----

func parsePostgres(raw string) ([]PlanNode, error) {
	var top any
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return nil, fmt.Errorf("PostgreSQL 计划不是 JSON: %w", err)
	}
	var plan map[string]any
	switch v := top.(type) {
	case []any:
		if len(v) == 0 {
			return nil, errors.New("PostgreSQL 计划为空")
		}
		m, _ := v[0].(map[string]any)
		plan, _ = m["Plan"].(map[string]any)
	case map[string]any:
		plan, _ = v["Plan"].(map[string]any)
	}
	if plan == nil {
		return nil, errors.New("PostgreSQL 计划缺少 Plan")
	}
	return []PlanNode{pgNode(plan)}, nil
}

func pgNode(m map[string]any) PlanNode {
	n := PlanNode{
		Op:          text(m["Node Type"]),
		Relation:    text(m["Relation Name"]),
		StartupCost: number(m["Startup Cost"]),
		Cost:        number(m["Total Cost"]),
		Rows:        number(m["Plan Rows"]),
		Actual:      time.Duration(number(m["Actual Total Time"]) * float64(time.Millisecond)),
		ActualRows:  number(m["Actual Rows"]),
		Loops:       number(m["Actual Loops"]),
	}
	var details []string
	for _, key := range []string{"Join Type", "Index Name", "Filter", "Index Cond", "Hash Cond", "Sort Key", "Sort Method"} {
		if v := text(m[key]); v != "" {
			details = append(details, key+": "+v)
		}
	}
	n.Detail = strings.Join(details, "; ")
	n.Buffers = pgBuffers(m)
	if plans, ok := m["Plans"].([]any); ok {
		for _, p := range plans {
			if child, ok := p.(map[string]any); ok {
				n.Children = append(n.Children, pgNode(child))
			}
		}
	}
	return n
}

// pgBuffers 把 PostgreSQL EXPLAIN (ANALYZE, BUFFERS) 的块统计压成一行。
func pgBuffers(m map[string]any) string {
	var parts []string
	add := func(label, key string) {
		if v := number(m[key]); v > 0 {
			parts = append(parts, fmt.Sprintf("%s=%.0f", label, v))
		}
	}
	add("shared hit", "Shared Hit Blocks")
	add("shared read", "Shared Read Blocks")
	add("shared dirtied", "Shared Dirtied Blocks")
	add("shared written", "Shared Written Blocks")
	add("local hit", "Local Hit Blocks")
	add("local read", "Local Read Blocks")
	add("temp read", "Temp Read Blocks")
	add("temp written", "Temp Written Blocks")
	if len(parts) == 0 {
		return ""
	}
	return "buffers: " + strings.Join(parts, " ")
}

// ---- MySQL JSON ----

func parseMySQL(raw string) ([]PlanNode, error) {
	var top map[string]any
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return nil, fmt.Errorf("MySQL 计划不是 JSON: %w", err)
	}
	qb, _ := top["query_block"].(map[string]any)
	if qb == nil {
		return nil, errors.New("MySQL 计划缺少 query_block")
	}
	return []PlanNode{mysqlBlock(qb)}, nil
}

func mysqlBlock(m map[string]any) PlanNode {
	n := PlanNode{Op: "Query Block"}
	if ci, ok := m["cost_info"].(map[string]any); ok {
		n.Cost = number(ci["query_cost"])
	}
	if t, ok := m["table"].(map[string]any); ok {
		n.Children = append(n.Children, mysqlTable(t))
	}
	for _, key := range []string{
		"ordering_operation", "grouping_operation", "duplicates_removal",
		"buffer_result", "union_result", "windowing", "materialized_from_subquery",
		"optimized_away_subqueries", "subqueries",
	} {
		if child, ok := m[key].(map[string]any); ok {
			c := mysqlBlock(child)
			c.Op = prettyKey(key)
			n.Children = append(n.Children, c)
		}
	}
	if loops, ok := m["nested_loop"].([]any); ok {
		loop := PlanNode{Op: "Nested Loop"}
		for _, item := range loops {
			if im, ok := item.(map[string]any); ok {
				if t, ok := im["table"].(map[string]any); ok {
					loop.Children = append(loop.Children, mysqlTable(t))
				}
			}
		}
		n.Children = append(n.Children, loop)
	}
	for _, key := range []string{"attached_subqueries", "select_list_subqueries", "having_subqueries"} {
		if arr, ok := m[key].([]any); ok {
			for _, item := range arr {
				if im, ok := item.(map[string]any); ok {
					n.Children = append(n.Children, mysqlBlock(im))
				}
			}
		}
	}
	return n
}

func mysqlTable(m map[string]any) PlanNode {
	n := PlanNode{Op: "Table", Relation: text(m["table_name"])}
	if at := text(m["access_type"]); at != "" {
		n.Op = strings.ToUpper(at)
	}
	if ci, ok := m["cost_info"].(map[string]any); ok {
		n.Cost = number(ci["read_cost"]) + number(ci["eval_cost"])
	}
	if v, ok := m["rows_examined_per_scan"]; ok {
		n.Rows = number(v)
	} else {
		n.Rows = number(m["rows"])
	}
	var details []string
	for _, key := range []string{
		"key", "possible_keys", "key_length", "ref", "filtered", "using_index",
		"attached_condition", "rows_examined_per_scan", "rows_produced_per_join", "data_read_per_join",
	} {
		if v := text(m[key]); v != "" {
			details = append(details, key+": "+v)
		}
	}
	n.Detail = strings.Join(details, "; ")
	return n
}

func prettyKey(key string) string {
	parts := strings.Split(key, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// ---- ClickHouse ----

func parseClickHouse(raw string) ([]PlanNode, error) {
	var top any
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		// 有些版本返回多行文本/JSON Lines，统一按缩进树解析。
		if nodes := parseTree(raw); len(nodes) > 0 {
			return nodes, nil
		}
		return nil, fmt.Errorf("ClickHouse 计划无法解析: %w", err)
	}
	switch v := top.(type) {
	case []any:
		var out []PlanNode
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, genericNode(m))
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	case map[string]any:
		if p, ok := v["Plan"].(map[string]any); ok {
			return []PlanNode{genericNode(p)}, nil
		}
		if p, ok := v["plan"].(map[string]any); ok {
			return []PlanNode{genericNode(p)}, nil
		}
		return []PlanNode{genericNode(v)}, nil
	}
	return nil, errors.New("ClickHouse 计划为空")
}

func genericNode(m map[string]any) PlanNode {
	n := PlanNode{
		Op:          firstText(m, "Node Type", "node_type", "type", "name", "expression", "description"),
		Relation:    firstText(m, "Relation Name", "table", "table_name"),
		StartupCost: firstNumber(m, "Startup Cost", "startup_cost"),
		Cost:        firstNumber(m, "Cost", "cost", "Total Cost", "Estimated Cost", "estimated_cost"),
		Rows:        firstNumber(m, "Rows", "rows", "Plan Rows", "Estimated Rows", "estimated_rows"),
		ActualRows:  firstNumber(m, "Actual Rows", "actual_rows"),
		Loops:       firstNumber(m, "Actual Loops", "actual_loops", "loops"),
		Detail:      firstText(m, "Detail", "detail", "description", "Expression", "Header", "Step", "Sort Description", "Condition"),
	}
	for _, key := range []string{"Plans", "plans", "Children", "children", "Steps", "steps"} {
		if arr, ok := m[key].([]any); ok {
			for _, item := range arr {
				if child, ok := item.(map[string]any); ok {
					n.Children = append(n.Children, genericNode(child))
				}
			}
		}
	}
	return n
}

func firstText(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := text(m[k]); v != "" {
			return v
		}
	}
	return ""
}

func firstNumber(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return number(v)
		}
	}
	return 0
}

// ---- SQLite / text trees ----

func parseSQLite(columns []string, rows [][]any) ([]PlanNode, error) {
	idx := map[string]int{}
	for i, c := range columns {
		idx[strings.ToLower(c)] = i
	}
	idCol, parentCol, detailCol := idx["id"], idx["parent"], idx["detail"]
	if _, ok := idx["detail"]; !ok {
		return nil, errors.New("SQLite 计划缺少 detail 列")
	}
	type entry struct {
		id, parent int
		node       PlanNode
	}
	entries := make([]entry, 0, len(rows))
	for _, row := range rows {
		id, parent := 0, 0
		if idCol < len(row) {
			id = int(number(row[idCol]))
		}
		if parentCol < len(row) {
			parent = int(number(row[parentCol]))
		}
		detail := ""
		if detailCol < len(row) {
			detail = cell([][]any{row}, 0, detailCol)
		}
		node := PlanNode{Op: detail, Detail: detail}
		entries = append(entries, entry{id: id, parent: parent, node: node})
	}
	byID := map[int]int{}
	for i := range entries {
		byID[entries[i].id] = i
	}
	var roots []int
	for i := range entries {
		e := &entries[i]
		if e.parent != 0 {
			if p, ok := byID[e.parent]; ok {
				entries[p].node.Children = append(entries[p].node.Children, e.node)
				continue
			}
		}
		roots = append(roots, i)
	}
	if len(roots) == 0 {
		for i := range entries {
			roots = append(roots, i)
		}
	}
	out := make([]PlanNode, 0, len(roots))
	for _, i := range roots {
		out = append(out, entries[i].node)
	}
	return out, nil
}

var (
	treeCostRe       = regexp.MustCompile(`cost=([0-9.]+)`)
	treeRowsRe       = regexp.MustCompile(`rows=([0-9.]+)`)
	treeActualRe     = regexp.MustCompile(`actual time=([0-9.]+)`)
	treeActualRowsRe = regexp.MustCompile(`actual time=[^)]*rows=([0-9.]+)`)
	treeLoopsRe      = regexp.MustCompile(`loops=([0-9.]+)`)
)

func parseTree(raw string) []PlanNode {
	type level struct {
		indent int
		index  int
	}
	var roots []PlanNode
	var stack []level
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.Contains(line, "->") && !strings.HasPrefix(trimmed, "Table scan") &&
			!strings.HasPrefix(trimmed, "Filter") && !strings.HasPrefix(trimmed, "Aggregating") {
			if len(roots) == 0 && !strings.Contains(raw, "->") {
				return nil
			}
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		body := trimmed
		if i := strings.LastIndex(body, "->"); i >= 0 {
			body = strings.TrimSpace(body[i+2:])
		}
		node := PlanNode{Op: body, Detail: body}
		if m := treeCostRe.FindStringSubmatch(body); len(m) == 2 {
			node.Cost, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := treeRowsRe.FindStringSubmatch(body); len(m) == 2 {
			node.Rows, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := treeActualRe.FindStringSubmatch(body); len(m) == 2 {
			ms, _ := strconv.ParseFloat(m[1], 64)
			node.Actual = time.Duration(ms * float64(time.Millisecond))
		}
		if m := treeActualRowsRe.FindStringSubmatch(body); len(m) == 2 {
			node.ActualRows, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := treeLoopsRe.FindStringSubmatch(body); len(m) == 2 {
			node.Loops, _ = strconv.ParseFloat(m[1], 64)
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, node)
			stack = append(stack, level{indent: indent, index: len(roots) - 1})
		} else {
			p := &roots
			for _, l := range stack {
				p = &(*p)[l.index].Children
			}
			*p = append(*p, node)
			stack = append(stack, level{indent: indent, index: len(*p) - 1})
		}
	}
	return roots
}

// Text 把计划树渲染成给模型/日志看的缩进文本。
func Text(nodes []PlanNode) string {
	var b strings.Builder
	var walk func(nodes []PlanNode, depth int)
	walk = func(nodes []PlanNode, depth int) {
		for _, n := range nodes {
			b.WriteString(strings.Repeat("  ", depth))
			b.WriteString(n.Op)
			if n.Relation != "" {
				b.WriteString(" on " + n.Relation)
			}
			if n.StartupCost > 0 {
				fmt.Fprintf(&b, " startup=%.2f", n.StartupCost)
			}
			if n.Cost > 0 {
				fmt.Fprintf(&b, " cost=%.2f", n.Cost)
			}
			if n.Rows > 0 {
				fmt.Fprintf(&b, " rows=%.0f", n.Rows)
			}
			if n.Actual > 0 {
				fmt.Fprintf(&b, " actual=%s", n.Actual)
			}
			if n.ActualRows > 0 {
				fmt.Fprintf(&b, " actual_rows=%.0f", n.ActualRows)
			}
			if n.Loops > 0 {
				fmt.Fprintf(&b, " loops=%.0f", n.Loops)
			}
			if n.Buffers != "" {
				b.WriteString(" " + n.Buffers)
			}
			if n.Detail != "" && n.Detail != n.Op {
				b.WriteString(" | " + n.Detail)
			}
			b.WriteByte('\n')
			walk(n.Children, depth+1)
		}
	}
	walk(nodes, 0)
	return strings.TrimRight(b.String(), "\n")
}
