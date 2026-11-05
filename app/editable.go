package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ColumnInfo 是表的一列元数据（可编辑判定用）。
type ColumnInfo struct {
	Name       string
	Type       string
	PrimaryKey bool
}

// EditPlan 描述「这个结果是否可写、怎么写」。
type EditPlan struct {
	Kind     string
	Table    string
	Columns  []string // 结果列（必须都能在表列里找到）
	Editable []bool   // 每列是否可改（主键列不可改）
	PKIdx    []int    // 主键列在结果里的下标
	PKNames  []string
}

var (
	ErrNoPrimaryKey = errors.New("表没有主键，结果只读")
	ErrColumnRange  = errors.New("结果列与表列不匹配，结果只读")
)

// BuildEditPlan 判定结果能否写：单表、结果列都来自该表、主键列全部在结果里。
func BuildEditPlan(kind, table string, resultCols []string, tableCols []ColumnInfo) (*EditPlan, error) {
	if table == "" {
		return nil, ErrColumnRange
	}
	if len(tableCols) == 0 {
		return nil, ErrColumnRange
	}
	byName := map[string]ColumnInfo{}
	for _, c := range tableCols {
		byName[strings.ToLower(c.Name)] = c
	}
	var pkNames []string
	for _, c := range tableCols {
		if c.PrimaryKey {
			pkNames = append(pkNames, c.Name)
		}
	}
	if len(pkNames) == 0 {
		return nil, ErrNoPrimaryKey
	}

	plan := &EditPlan{Kind: kind, Table: table, Columns: append([]string(nil), resultCols...)}
	seen := map[string]bool{}
	for _, name := range resultCols {
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("%w：结果列 %q 重复", ErrColumnRange, name)
		}
		seen[key] = true
		col, ok := byName[key]
		if !ok {
			return nil, fmt.Errorf("%w：结果列 %q 不在表 %s 里（别名/表达式只读）", ErrColumnRange, name, table)
		}
		plan.Editable = append(plan.Editable, !col.PrimaryKey)
	}
	for _, pk := range pkNames {
		idx := -1
		for i, name := range resultCols {
			if strings.EqualFold(name, pk) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("%w：结果里没有主键列 %q", ErrColumnRange, pk)
		}
		plan.PKIdx = append(plan.PKIdx, idx)
		plan.PKNames = append(plan.PKNames, pk)
	}
	return plan, nil
}

// CellKey 是一个结果单元格（行下标 + 结果列下标）。
type CellKey struct{ Row, Col int }

// EditSet 是结果区的本地改动（不落库、不重跑 SQL）。
type EditSet struct {
	Cells   map[CellKey]any
	Inserts []map[int]any
	Deletes map[int]bool
}

func NewEditSet() *EditSet {
	return &EditSet{Cells: map[CellKey]any{}, Deletes: map[int]bool{}}
}

// Count 返回改动数（改格 + 新增行 + 删除行）。
func (e *EditSet) Count() int { return len(e.Cells) + len(e.Inserts) + len(e.Deletes) }

func (e *EditSet) SetCell(row, col int, v any) {
	e.Cells[CellKey{Row: row, Col: col}] = v
}

func (e *EditSet) ClearCell(row, col int) { delete(e.Cells, CellKey{Row: row, Col: col}) }

func (e *EditSet) MarkDeleted(row int) {
	e.Deletes[row] = true
	for k := range e.Cells {
		if k.Row == row {
			delete(e.Cells, k) // 既改又删：以删为准
		}
	}
}

func (e *EditSet) RestoreRow(row int) { delete(e.Deletes, row) }

// AddInsert 追加一行待新增（返回其下标）；未填的列走库默认。
func (e *EditSet) AddInsert() int {
	e.Inserts = append(e.Inserts, map[int]any{})
	return len(e.Inserts) - 1
}

func (e *EditSet) RemoveInsert(i int) {
	if i < 0 || i >= len(e.Inserts) {
		return
	}
	e.Inserts = append(e.Inserts[:i], e.Inserts[i+1:]...)
}

// Clear 丢弃全部本地改动。
func (e *EditSet) Clear() {
	e.Cells = map[CellKey]any{}
	e.Inserts = nil
	e.Deletes = map[int]bool{}
}

// Op 是一条待执行的写操作。
type Op struct {
	SQL  string
	Args []any
	Want int64 // 期望影响行数；0 表示不检查
}

// TxLike 是 ApplyOps 需要的最小事务能力（engine.Tx 满足它）。
type TxLike interface {
	Exec(ctx context.Context, query string, args ...any) (int64, error)
}

// BuildOps 生成参数化写操作（顺序：UPDATE → INSERT → DELETE）。
func (e *EditSet) BuildOps(plan *EditPlan, rows [][]any) ([]Op, error) {
	if plan == nil {
		return nil, ErrColumnRange
	}
	var ops []Op

	// UPDATE：改格；跳过被删除的行与「值与原来相同」的格。
	keys := make([]CellKey, 0, len(e.Cells))
	for k := range e.Cells {
		if e.Deletes[k.Row] {
			continue
		}
		if k.Row < 0 || k.Row >= len(rows) || k.Col < 0 || k.Col >= len(plan.Columns) {
			continue
		}
		if !plan.Editable[k.Col] {
			continue
		}
		old := cellValue(rows[k.Row], k.Col)
		if equalCell(old, e.Cells[k]) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Row != keys[j].Row {
			return keys[i].Row < keys[j].Row
		}
		return keys[i].Col < keys[j].Col
	})
	for _, k := range keys {
		where, whereArgs, err := pkWhere(plan, rows[k.Row], 2)
		if err != nil {
			return nil, err
		}
		set := QuoteIdent(plan.Kind, plan.Columns[k.Col]) + " = " + Placeholder(plan.Kind, 1)
		args := []any{e.Cells[k]}
		args = append(args, whereArgs...)
		ops = append(ops, Op{
			SQL:  fmt.Sprintf("UPDATE %s SET %s WHERE %s", QuoteIdent(plan.Kind, plan.Table), set, where),
			Args: args,
			Want: 1,
		})
	}

	// INSERT：只写已填的列。
	for i, ins := range e.Inserts {
		cols := make([]int, 0, len(ins))
		for c := range ins {
			if c >= 0 && c < len(plan.Columns) && plan.Editable[c] {
				cols = append(cols, c)
			}
		}
		if len(cols) == 0 {
			return nil, fmt.Errorf("新增行 %d 没有填任何值", i+1)
		}
		sort.Ints(cols)
		names := make([]string, len(cols))
		marks := make([]string, len(cols))
		args := make([]any, len(cols))
		for j, c := range cols {
			names[j] = QuoteIdent(plan.Kind, plan.Columns[c])
			marks[j] = Placeholder(plan.Kind, j+1)
			args[j] = ins[c]
		}
		ops = append(ops, Op{
			SQL:  fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", QuoteIdent(plan.Kind, plan.Table), strings.Join(names, ", "), strings.Join(marks, ", ")),
			Args: args,
			Want: 1,
		})
	}

	// DELETE：按主键。
	delRows := make([]int, 0, len(e.Deletes))
	for r := range e.Deletes {
		if r >= 0 && r < len(rows) {
			delRows = append(delRows, r)
		}
	}
	sort.Ints(delRows)
	for _, r := range delRows {
		where, args, err := pkWhere(plan, rows[r], 1)
		if err != nil {
			return nil, err
		}
		ops = append(ops, Op{
			SQL:  fmt.Sprintf("DELETE FROM %s WHERE %s", QuoteIdent(plan.Kind, plan.Table), where),
			Args: args,
			Want: 1,
		})
	}
	return ops, nil
}

// Preview 把操作渲染成可读 SQL（内联字面量，只用于确认弹窗）。
func (e *EditSet) Preview(plan *EditPlan, rows [][]any) ([]string, error) {
	ops, err := e.BuildOps(plan, rows)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = renderOp(op)
	}
	return out, nil
}

// ApplyOps 逐条执行；任一失败或影响行数不符即停止（调用方负责回滚）。
func ApplyOps(ctx context.Context, tx TxLike, ops []Op) error {
	for i, op := range ops {
		n, err := tx.Exec(ctx, op.SQL, op.Args...)
		if err != nil {
			return fmt.Errorf("第 %d 条失败: %w", i+1, err)
		}
		if op.Want > 0 && n != op.Want {
			return fmt.Errorf("第 %d 条影响 %d 行（期望 %d），已回滚", i+1, n, op.Want)
		}
	}
	return nil
}

// pkWhere 生成「全部主键」的 WHERE 子句与参数；start 是占位符起始序号（PostgreSQL 用）。
func pkWhere(plan *EditPlan, row []any, start int) (string, []any, error) {
	if len(plan.PKIdx) == 0 {
		return "", nil, ErrNoPrimaryKey
	}
	parts := make([]string, len(plan.PKIdx))
	args := make([]any, len(plan.PKIdx))
	for i, idx := range plan.PKIdx {
		parts[i] = QuoteIdent(plan.Kind, plan.PKNames[i]) + " = " + Placeholder(plan.Kind, start+i)
		args[i] = cellValue(row, idx)
	}
	return strings.Join(parts, " AND "), args, nil
}

func cellValue(row []any, col int) any {
	if col < 0 || col >= len(row) {
		return nil
	}
	return row[col]
}

// equalCell 比较两个单元格值（[]byte 按文本/字节比较）。
func equalCell(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	as, aok := textOf(a)
	bs, bok := textOf(b)
	if aok && bok {
		return as == bs
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func textOf(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case []byte:
		return string(t), true
	}
	return "", false
}

// renderOp 把占位符（? 或 $n）依次替换成内联字面量（预览用）。
func renderOp(op Op) string {
	s := op.SQL
	if strings.Contains(s, "$1") {
		for i, arg := range op.Args {
			s = strings.Replace(s, fmt.Sprintf("$%d", i+1), QuoteValue(arg), 1)
		}
	} else {
		for _, arg := range op.Args {
			if j := strings.IndexByte(s, '?'); j >= 0 {
				s = s[:j] + QuoteValue(arg) + s[j+1:]
			}
		}
	}
	return strings.TrimSuffix(s, ";")
}

// Placeholder 返回第 n 个参数占位符（从 1 计数）：PostgreSQL 用 $n，其余用 ?。
func Placeholder(kind string, n int) string {
	if kind == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}
