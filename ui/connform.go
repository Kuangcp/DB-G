package ui

// connFormState 是新建/编辑连接弹窗的表单状态。
// 纯瞬态：不落盘，关闭弹窗即丢弃；保存时由 connFormProfile 读成 app.ConnProfile。
type connFormState struct {
	Open     bool
	URL      string // 从 JDBC/URL 导入的临时输入（不落盘）
	URLErr   string
	ID       string
	Name     string
	Path     string
	Err      string
	Kind     string
	Host     string
	Port     string
	User     string
	Pass     string
	ShowPass bool // 密码框是否显示明文
	DB       string
	Params   string
	Timeout  string // 查询超时（秒，空 = 默认 30）
	Guard    bool   // 高危 SQL 硬阻断
	AIStrict bool   // 连接级 AI 只发 schema + SQL
	Folder   string // 连接所属文件夹；空 = 根级
	Tags     []connTagDraft
	TagInput string // 标签输入框正文
	TagColor string // 下一个新标签使用的调色板键
}
