package app

import (
	"runtime/debug"
	"strings"
)

// Version 返回构建版本：有正式 tag 就是 vX.Y.Z-<短哈希>，否则 dev-<短哈希>，
// 工作区有未提交改动时再带 +dirty。取不到 VCS 信息就退回 "dev"。
func Version() string {
	const fallback = "dev"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallback
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
	ver := strings.TrimPrefix(info.Main.Version, "v")
	// 伪版本（v0.0.0-2026…-abc）没有意义，只留短哈希。
	if ver == "(devel)" || ver == "" || strings.HasPrefix(ver, "0.0.0-") {
		ver = ""
	}
	var out string
	switch {
	case ver != "" && rev != "":
		out = "v" + ver + "-" + rev
	case rev != "":
		out = "dev-" + rev
	case ver != "":
		out = "v" + ver
	default:
		return fallback
	}
	if modified {
		out += "+dirty"
	}
	return out
}
