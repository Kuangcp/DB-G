package sync

// 仓库 DTO。字段与 app.ConnProfile / app.ConsoleRecord 对齐，但独立演进：
// 加密格式与同步字段不能和导出/导入文件互相绑架。
const FormatVersion = 1

// Tag 是连接上的彩色标签。
type Tag struct {
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
}

// Conn 是仓库内一条连接（明文结构，落盘前整条加密）。
type Conn struct {
	FormatVersion int    `json:"formatVersion"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Path          string `json:"path,omitempty"`
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	User          string `json:"user,omitempty"`
	Password      string `json:"password,omitempty"`
	Database      string `json:"database,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	Params        string `json:"params,omitempty"`
	FolderID      string `json:"folderID,omitempty"`
	QueryTimeout  int    `json:"queryTimeout,omitempty"`
	GuardBlock    bool   `json:"guardBlock,omitempty"`
	Tags          []Tag  `json:"tags,omitempty"`
}

// Folder 是同步的文件夹。
type Folder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentID,omitempty"`
}

// Workspace 是同步的工作区（成员顺序即标签顺序）。
type Workspace struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	AutoNamed           bool     `json:"autoNamed,omitempty"`
	SortOrder           int      `json:"sortOrder,omitempty"`
	CreatedAt           int64    `json:"createdAt,omitempty"`
	LastActiveConsoleID string   `json:"lastActiveConsoleID,omitempty"`
	Members             []string `json:"members,omitempty"`
}

// ConsoleMeta 是同步的控制台元数据；设备本地的 filePath/caret/external 不进来。
type ConsoleMeta struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionID"`
	Name         string `json:"name"`
	SortOrder    int    `json:"sortOrder,omitempty"`
	CreatedAt    int64  `json:"createdAt,omitempty"`
	UpdatedAt    int64  `json:"updatedAt,omitempty"`
}

// Console 是一个控制台元数据 + 正文。
type Console struct {
	Meta ConsoleMeta
	Body string
}

// Snapshot 是导出/导入的全量同步态。
type Snapshot struct {
	Conns      []Conn
	Folders    []Folder
	Workspaces []Workspace
	Consoles   []Console
}
