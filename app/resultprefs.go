package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"db-g/engine"
)

// ResultPrefs 是查询结果的展示预算设置。
type ResultPrefs struct {
	// BudgetMB 是结果集文本/字节总量上限（MB）；超过后停止加载并标记截断。
	BudgetMB int `json:"budgetMB"`
}

const (
	DefaultResultBudgetMB = engine.DefaultResultBudgetMB
	MinResultBudgetMB     = engine.MinResultBudgetMB
	MaxResultBudgetMB     = engine.MaxResultBudgetMB
)

// DefaultResultPrefs 返回默认结果设置：8 MB。
func DefaultResultPrefs() ResultPrefs {
	return ResultPrefs{BudgetMB: DefaultResultBudgetMB}
}

// NormalizeResultPrefs 把缺省/越界的预算回退到默认值或允许范围。
func NormalizeResultPrefs(p ResultPrefs) ResultPrefs {
	if p.BudgetMB == 0 {
		p.BudgetMB = DefaultResultBudgetMB
	}
	if p.BudgetMB < MinResultBudgetMB {
		p.BudgetMB = MinResultBudgetMB
	}
	if p.BudgetMB > MaxResultBudgetMB {
		p.BudgetMB = MaxResultBudgetMB
	}
	return p
}

// DefaultResultPrefsPath 返回结果设置路径（与连接库同目录）。
func DefaultResultPrefsPath() string {
	return filepath.Join(AppConfigDir(), "results.json")
}

// LoadResultPrefs 读结果设置；文件不存在或损坏时用默认值。
func LoadResultPrefs(path string) (ResultPrefs, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultResultPrefs(), ""
		}
		return DefaultResultPrefs(), "读取结果设置失败: " + err.Error()
	}
	prefs := DefaultResultPrefs()
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultResultPrefs(), "结果设置损坏，已用默认值: " + err.Error()
	}
	return NormalizeResultPrefs(prefs), ""
}

// SaveResultPrefs 写回结果设置（0600，先写临时文件再改名）。
func SaveResultPrefs(path string, prefs ResultPrefs) error {
	prefs = NormalizeResultPrefs(prefs)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
