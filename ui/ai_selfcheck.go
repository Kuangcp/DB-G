package ui

import (
	"sort"
	"strings"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
	dbsql "db-g/sql"
)

// aiSQLIssue 是 AI 生成 SQL 在本地 schema 上的一个高置信度问题。
type aiSQLIssue struct {
	Table  string
	Column string
}

// aiSQLReport 是一次本地自检结果：表列问题 + 高危规则。
type aiSQLReport struct {
	SQL    string
	Issues []aiSQLIssue
	Risks  []app.SQLRisk
}

func (r aiSQLReport) Empty() bool { return len(r.Issues) == 0 && len(r.Risks) == 0 }

// Summary 用一行文本概括自检结果。
func (r aiSQLReport) Summary() string {
	if r.Empty() {
		return ""
	}
	parts := make([]string, 0, len(r.Risks)+len(r.Issues))
	for _, risk := range r.Risks {
		parts = append(parts, risk.Message)
	}
	for _, issue := range r.Issues {
		if issue.Column == "" {
			parts = append(parts, locales.AiSelfCheckUnknownTable(issue.Table))
		} else {
			parts = append(parts, locales.AiSelfCheckUnknownColumn(issue.Column, issue.Table))
		}
	}
	return locales.AiSelfCheck(strings.Join(parts, " · "))
}

// HasRisk 报告是否存在高危规则命中（用于危险色提示）。
func (r aiSQLReport) HasRisk() bool { return len(r.Risks) > 0 }

// aiSQLCheck 汇总表列自检与确定性高危规则。
func (a *root) aiSQLCheck(sql string) aiSQLReport {
	return aiSQLReport{SQL: sql, Issues: a.aiSQLLint(sql), Risks: app.CheckSQLRisks(sql)}
}

// aiSQLLint 用当前连接的已知表/缓存列做保守自检：
// 只报“表不存在”和能确定归属的列不存在，上下文没加载时不乱报。
func (a *root) aiSQLLint(sql string) []aiSQLIssue {
	connID := a.activeConnID()
	if connID == "" || strings.TrimSpace(sql) == "" {
		return nil
	}
	key, schema := a.completionScope(connID)
	tables, views, ok := a.session.Catalog.objects(key)
	if !ok || len(tables)+len(views) == 0 {
		return nil // 目录未加载时不做猜测
	}
	known := map[string]engine.ObjectKind{}
	for _, name := range tables {
		known[strings.ToLower(name)] = engine.ObjectTable
	}
	for _, name := range views {
		known[strings.ToLower(name)] = engine.ObjectView
	}
	rels := dbsql.Relations(sql)
	if len(rels) == 0 {
		return nil
	}
	toks := dbsql.Tokenize(sql)
	cte := dbsql.CTENames(toks)

	aliasToTable := map[string]string{}
	tableNames := map[string]string{} // lower -> display
	var issues []aiSQLIssue
	seen := map[string]bool{}
	add := func(table, column string) {
		key := strings.ToLower(table) + "\x00" + strings.ToLower(column)
		if seen[key] {
			return
		}
		seen[key] = true
		issues = append(issues, aiSQLIssue{Table: table, Column: column})
	}
	columns := map[string]map[string]bool{} // lower table -> lower columns
	loaded := map[string]bool{}
	for _, rel := range rels {
		if rel.Table == "" || cte[strings.ToLower(rel.Table)] {
			continue
		}
		lower := strings.ToLower(rel.Table)
		if rel.Schema != "" && schema != "" && !strings.EqualFold(rel.Schema, schema) {
			continue // 其他 schema 无法用当前目录证明不存在
		}
		if _, ok := known[lower]; !ok {
			add(rel.Table, "")
			continue
		}
		tableNames[lower] = rel.Table
		alias := lower
		if rel.Alias != "" {
			alias = strings.ToLower(rel.Alias)
		}
		aliasToTable[alias] = lower
		aliasToTable[lower] = lower
		if cols, ok := a.session.Catalog.columns(key, rel.Table); ok {
			set := map[string]bool{}
			for _, c := range cols {
				set[strings.ToLower(c.Name)] = true
			}
			columns[lower] = set
			loaded[lower] = true
		}
	}
	// 限定列：alias.col / table.col。
	skip := map[int]bool{}
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].Kind != 'i' || toks[i+1].Text != "." || toks[i+2].Kind != 'i' {
			continue
		}
		left, col := strings.ToLower(toks[i].Text), toks[i+2].Text
		table, ok := aliasToTable[left]
		if !ok {
			continue // schema.table / 未知限定，交给表检查或跳过
		}
		skip[i], skip[i+1], skip[i+2] = true, true, true
		if set, ok := columns[table]; ok && !set[strings.ToLower(col)] {
			add(tableNames[table], col)
		}
	}
	// 裸列只在能完整归属时检查：单表，或所有引用表都有列缓存。
	verifiable := len(columns) > 0
	for lower := range tableNames {
		if !loaded[lower] {
			verifiable = false
			break
		}
	}
	if verifiable {
		keywords := sqlCheckKeywords()
		colAliases := map[string]bool{}
		for i := 0; i+1 < len(toks); i++ {
			if toks[i].Kind == 'i' && strings.EqualFold(toks[i].Text, "as") && toks[i+1].Kind == 'i' {
				colAliases[strings.ToLower(toks[i+1].Text)] = true
			}
		}
		for i, tok := range toks {
			if tok.Kind != 'i' || skip[i] {
				continue
			}
			name := strings.ToLower(tok.Text)
			if keywords[name] || aliasToTable[name] != "" || cte[name] || colAliases[name] {
				continue
			}
			if i+1 < len(toks) && (toks[i+1].Text == "(" || toks[i+1].Text == ".") {
				continue
			}
			if i > 0 && toks[i-1].Text == "." {
				continue
			}
			found := false
			for _, set := range columns {
				if set[name] {
					found = true
					break
				}
			}
			if !found {
				names := make([]string, 0, len(tableNames))
				for _, display := range tableNames {
					names = append(names, display)
				}
				sort.Strings(names)
				add(strings.Join(names, ","), tok.Text)
			}
		}
	}
	if len(issues) > 8 {
		issues = issues[:8]
	}
	return issues
}

// sqlCheckKeywords 是裸标识符自检时跳过的词表。
func sqlCheckKeywords() map[string]bool {
	out := map[string]bool{}
	for _, words := range []string{sqlKeywordWords, sqlTypeWords, sqlConstWords, sqlFuncWords} {
		for _, w := range strings.Fields(words) {
			out[strings.ToLower(w)] = true
		}
	}
	for _, w := range strings.Fields(`over partition range rows preceding following nulls first last
		window qualify lateral tablesample offset fetch next only for update share nowait skip locked`) {
		out[w] = true
	}
	return out
}

// aiSQLCheckText 兼容旧调用：只根据表列问题拼一行提示。
func aiSQLCheckText(issues []aiSQLIssue) string {
	return aiSQLReport{Issues: issues}.Summary()
}
