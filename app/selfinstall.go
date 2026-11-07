package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// releaseBuild 由发布构建用 -ldflags "-X db-g/app.releaseBuild=1" 打开。
// 只有正式分发包才启用自举安装/升级；go run 与测试默认关闭，避免开发期动用户目录。
var releaseBuild string

// ReleaseBuild 报告当前二进制是否是发布构建（启用自举安装/升级）。
func ReleaseBuild() bool { return releaseBuild == "1" }

// ParseBuildNumber 把 "1.2.3" / "v1.2.3" 转换成数值 10203。
// minor 与 patch 必须在 0..99 内，否则会产生编码碰撞（1.0.100 与 1.1.0 都是 10100）。
func ParseBuildNumber(v string) (int, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return 0, false
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, false
		}
		nums[i] = n
	}
	if nums[1] > 99 || nums[2] > 99 {
		return 0, false
	}
	return nums[0]*10000 + nums[1]*100 + nums[2], true
}

// InstallStatus 描述自举安装/升级的当前状态，供 UI 决定是否显示安装图标。
type InstallStatus struct {
	BinPath     string // ~/.config/db-g/bin/db-g（或 XDG_CONFIG_HOME 下）
	VersionPath string // ~/.config/db-g/version
	DesktopPath string // ~/.local/share/applications/db-g.desktop
	IconPath    string // ~/.local/share/icons/db-g.png

	Running        string // 当前运行版本（display）
	Installed      string // version 文件里记录的版本（display）
	RunningBuild   int
	InstalledBuild int

	BinExists   bool
	Upgrade     bool // bin 已存在且运行版本更新 → 显示「升级」
	Repair      bool // 已安装同版本但 desktop/图标缺失 → 显示「修复」
	NeedInstall bool // 需要显示安装/升级图标
}

// CheckInstall 检查当前二进制是否需要安装到应用列表。
func CheckInstall() InstallStatus {
	st := InstallStatus{Running: Version()}
	cfg, err := installConfigDir()
	if err != nil {
		return st
	}
	st.BinPath = filepath.Join(cfg, "bin", "db-g")
	st.VersionPath = filepath.Join(cfg, "version")
	if data, err := installDataHome(); err == nil {
		st.DesktopPath = filepath.Join(data, "applications", "db-g.desktop")
		st.IconPath = filepath.Join(data, "icons", "db-g.png")
	}
	build, ok := ParseBuildNumber(st.Running)
	if !ok {
		// 版本号不可解析（开发构建）不做任何比较，也不显示图标。
		return st
	}
	st.RunningBuild = build
	if _, err := os.Stat(st.BinPath); err == nil {
		st.BinExists = true
	} else {
		st.NeedInstall = true
	}
	rec, err := loadVersionFile(st.VersionPath)
	if err != nil {
		st.NeedInstall = true
		return st
	}
	st.Installed, st.InstalledBuild = rec.Display, rec.Build
	if st.RunningBuild > rec.Build {
		st.NeedInstall = true
		st.Upgrade = st.BinExists
	}
	// 二进制与 version 都在但菜单项/图标丢了（例如旧版安装路径变更）：提示修复。
	if st.BinExists && !st.Upgrade {
		if !fileExists(st.DesktopPath) || !fileExists(st.IconPath) {
			st.NeedInstall = true
			st.Repair = true
		}
	}
	return st
}

// InstallSelf 把当前进程的二进制复制到配置目录，并写入 version、桌面菜单项与图标。
// 只有发布构建会调用（ReleaseBuild）；version 文件最后写，中途失败下次启动仍会提示安装。
func InstallSelf(iconPNG []byte) (InstallStatus, error) {
	st := CheckInstall()
	if st.BinPath == "" {
		return st, errors.New("无法确定配置目录")
	}
	if st.DesktopPath == "" || st.IconPath == "" {
		return st, errors.New("无法确定应用数据目录")
	}
	build, ok := ParseBuildNumber(Version())
	if !ok {
		return st, fmt.Errorf("版本号无法解析: %s", Version())
	}
	exe, err := os.Executable()
	if err != nil {
		return st, fmt.Errorf("无法定位当前程序: %w", err)
	}
	if err := installBinary(exe, st.BinPath); err != nil {
		return st, fmt.Errorf("复制程序失败: %w", err)
	}
	if len(iconPNG) == 0 {
		return st, errors.New("缺少图标数据")
	}
	if err := writeFileAtomic(st.IconPath, iconPNG, 0o644); err != nil {
		return st, fmt.Errorf("写入图标失败: %w", err)
	}
	if err := writeFileAtomic(st.DesktopPath, []byte(desktopEntry(st.BinPath, st.IconPath)), 0o644); err != nil {
		return st, fmt.Errorf("写入应用菜单项失败: %w", err)
	}
	rec := InstalledVersion{Build: build, Display: strings.TrimPrefix(Version(), "v")}
	if err := writeFileAtomic(st.VersionPath, []byte(versionFileContent(rec)), 0o644); err != nil {
		return st, fmt.Errorf("写入版本文件失败: %w", err)
	}
	st = CheckInstall()
	if st.NeedInstall {
		return st, errors.New("安装后校验失败")
	}
	return st, nil
}

// InstalledVersion 是 version 文件的 INI 内容。
type InstalledVersion struct {
	Build   int
	Display string
}

// versionFileContent 生成 version 文件：
//
//	[db-g]
//	build=10203
//	version=1.2.3
func versionFileContent(rec InstalledVersion) string {
	return fmt.Sprintf("[db-g]\nbuild=%d\nversion=%s\n", rec.Build, rec.Display)
}

// loadVersionFile 读取 version 文件；字段缺失或非法都视为不可用（会重新提示安装）。
func loadVersionFile(path string) (InstalledVersion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return InstalledVersion{}, err
	}
	var rec InstalledVersion
	hasBuild, hasDisplay := false, false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "build":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return InstalledVersion{}, fmt.Errorf("version 文件 build 非法: %q", value)
			}
			rec.Build, hasBuild = n, true
		case "version":
			rec.Display = strings.TrimSpace(value)
			hasDisplay = rec.Display != ""
		}
	}
	if !hasBuild || !hasDisplay {
		return InstalledVersion{}, errors.New("version 文件缺少 build/version 字段")
	}
	return rec, nil
}

// desktopEntry 生成 XDG 桌面菜单项；Exec/Icon 都用绝对路径，不依赖图标主题缓存。
func desktopEntry(execPath, iconPath string) string {
	lines := []string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.0",
		"Name=DB-G",
		"Comment=SQLite / MySQL / PostgreSQL / ClickHouse 数据库客户端",
		"Exec=" + desktopExecQuote(execPath),
		"Icon=" + iconPath,
		"Terminal=false",
		"Categories=Development;Database;",
		"StartupWMClass=db-g",
		"Keywords=SQL;Database;SQLite;MySQL;PostgreSQL;ClickHouse;",
		"",
	}
	return strings.Join(lines, "\n")
}

// desktopExecQuote 按 Desktop Entry 规范给 Exec 的路径加引号并转义。
func desktopExecQuote(path string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`, "%", "%%")
	return `"` + r.Replace(path) + `"`
}

// installConfigDir 返回配置目录（与 connections.json 同级），遵循 XDG_CONFIG_HOME。
func installConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "db-g"), nil
}

// installDataHome 返回 XDG 数据目录：优先 XDG_DATA_HOME，否则 ~/.local/share。
func installDataHome() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" && filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// installBinary 原子复制可执行文件：先写同目录临时文件再 rename。
// 直接覆盖正在运行的二进制会得到 ETXTBSY；rename 不会。
func installBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(dst), ".db-g-bin-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(0o755); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// fileExists 报告路径是否存在。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeFileAtomic 原子写文件（先临时文件再 rename），用于 version/desktop/图标。
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".db-g-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
