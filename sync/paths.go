package sync

import "strings"

// AppDir 是仓库内本应用的命名空间；同一仓库可被其他应用共用。
const AppDir = "db-g"

// KCVFile 是仓库内 KCV 的相对路径。
const KCVFile = AppDir + "/key.kcv"

// ConnectionPath 是一条连接的仓库相对路径。
func ConnectionPath(id string) string { return AppDir + "/connections/" + id + ".enc" }

// FolderPath 是一个文件夹的仓库相对路径。
func FolderPath(id string) string { return AppDir + "/folders/" + id + ".json" }

// WorkspacePath 是一个工作区的仓库相对路径。
func WorkspacePath(id string) string { return AppDir + "/workspaces/" + id + ".json" }

// ConsoleMetaPath 是一个控制台元数据的仓库相对路径。
func ConsoleMetaPath(id string) string { return AppDir + "/consoles/" + id + ".meta.json" }

// ConsoleBodyPath 是一个控制台正文的仓库相对路径。
func ConsoleBodyPath(id string) string { return AppDir + "/consoles/" + id + ".sql" }

// IsSQL 报告仓库相对路径是否是控制台正文。
func IsSQL(rel string) bool {
	return strings.HasPrefix(rel, AppDir+"/consoles/") && strings.HasSuffix(rel, ".sql")
}

// IsEntity 报告相对路径是否属于会被 sweep 管理的实体文件。
// 只认已知后缀，避免误删用户放进仓库的说明文件。
func IsEntity(rel string) bool {
	prefix := AppDir + "/"
	if !strings.HasPrefix(rel, prefix) {
		return false
	}
	switch {
	case strings.HasPrefix(rel, prefix+"connections/"):
		return strings.HasSuffix(rel, ".enc")
	case strings.HasPrefix(rel, prefix+"folders/"), strings.HasPrefix(rel, prefix+"workspaces/"):
		return strings.HasSuffix(rel, ".json")
	case strings.HasPrefix(rel, prefix+"consoles/"):
		return strings.HasSuffix(rel, ".sql") || strings.HasSuffix(rel, ".meta.json")
	}
	return false
}

// EntityPath 校验并返回实体文件的相对路径；不合法返回空串。
func EntityPath(dir, name, suffix string) string {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return ""
	}
	if !strings.HasSuffix(name, suffix) {
		return ""
	}
	return AppDir + "/" + dir + "/" + name
}
