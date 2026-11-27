package app

import (
	"path/filepath"

	"db-g/engine"
	"db-g/locales"
)

// ResultPrefs 是查询结果的展示预算设置。
type ResultPrefs struct {
	// BudgetMB 是结果集文本/字节总量上限（MB）；超过后停止加载并标记截断。
	BudgetMB int `json:"budgetMB"`
	// PageSize 是单页最多读取的行数；也是分页控件的每页行数。
	PageSize int `json:"pageSize"`
}

const (
	DefaultResultBudgetMB = engine.DefaultResultBudgetMB
	MinResultBudgetMB     = engine.MinResultBudgetMB
	MaxResultBudgetMB     = engine.MaxResultBudgetMB

	DefaultResultPageSize = engine.DefaultPageSize
	MinResultPageSize     = 50
	MaxResultPageSize     = 5000
)

// DefaultResultPrefs 返回默认结果设置：8 MB。
func DefaultResultPrefs() ResultPrefs {
	return ResultPrefs{BudgetMB: DefaultResultBudgetMB, PageSize: DefaultResultPageSize}
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
	if p.PageSize == 0 {
		p.PageSize = DefaultResultPageSize
	}
	if p.PageSize < MinResultPageSize {
		p.PageSize = MinResultPageSize
	}
	if p.PageSize > MaxResultPageSize {
		p.PageSize = MaxResultPageSize
	}
	return p
}

// DefaultResultPrefsPath 返回结果设置路径（与连接库同目录）。
func DefaultResultPrefsPath() string {
	return filepath.Join(AppConfigDir(), "results.json")
}

// LoadResultPrefs 读结果设置；文件不存在或损坏时用默认值。
func LoadResultPrefs(path string) (ResultPrefs, string) {
	prefs := DefaultResultPrefs()
	found, err := loadJSONFile(path, &prefs)
	if err != nil {
		if !found {
			return DefaultResultPrefs(), locales.AppResultReadFailed(err.Error())
		}
		return DefaultResultPrefs(), locales.AppResultCorrupt(err.Error())
	}
	if !found {
		return DefaultResultPrefs(), ""
	}
	return NormalizeResultPrefs(prefs), ""
}

// SaveResultPrefs 写回结果设置（0600，先写临时文件再改名）。
func SaveResultPrefs(path string, prefs ResultPrefs) error {
	return saveJSONFile(path, NormalizeResultPrefs(prefs), 0o755)
}
