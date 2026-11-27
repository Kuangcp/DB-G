package ui

import "db-g/app"

// installState 是自举安装/升级的状态（只在发布构建启用）。
type installState struct {
	Self    bool
	Need    app.InstallStatus
	Open    bool
	Busy    bool
	Err     string
	Note    string
	Install func([]byte) (app.InstallStatus, error) // 测试可注入；nil = app.InstallSelf
}
