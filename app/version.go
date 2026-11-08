package app

import (
	"runtime/debug"
	"strings"
)

// buildVersion 由构建用 -ldflags "-X db-g/app.buildVersion=$(git describe ...)" 注入，
// 开发构建（make run）也会注入；为空时才回退到二进制里的 VCS 信息。
var buildVersion string

// Version 返回构建版本，格式与安装后的 version 文件一致：
// 1.0.0-4-g7e186e8（去除 git describe 的 v 前缀；工作区有改动时带 -dirty）。
// 取不到构建信息就退回 "dev"。
func Version() string {
	if v := normalizeVersion(buildVersion); v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev string
	modified := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				rev = s.Value[:7]
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	ver := normalizeVersion(info.Main.Version)
	// 本地 go build 的伪版本（v1.0.1-0.2026…-hash）不是发布 tag，忽略。
	if ver == "(devel)" || strings.Contains(ver, "-0.20") {
		ver = ""
	}
	var out string
	switch {
	case ver != "" && rev != "":
		out = ver + "-" + rev
	case rev != "":
		out = "dev-" + rev
	case ver != "":
		out = ver
	default:
		return "dev"
	}
	if modified {
		out += "-dirty"
	}
	return out
}

// normalizeVersion 去掉首尾空白与 git describe 常见的 v 前缀。
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}
