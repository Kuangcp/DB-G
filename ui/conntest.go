package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// errPort 是端口填错的哨兵错误：调用方据此给中文提示。
var errPort = errors.New("端口必须是数字")

// connTestTimeout 是一次「测试连接」的上限。
const connTestTimeout = 5 * time.Second

// resetConnTest 清掉弹窗里那一行反馈（换类型、重新打开弹窗时）。
func (a *root) resetConnTest() {
	a.connTestMsg, a.connTestErr, a.connTestBusy = "", false, false
}

// testConnForm 用表单当前内容试连一次：先按类型建会话，Open（内含 ping）再跑
// 一条 SELECT 1，确认账号真能查。有窗口时在后台跑，结果回投到 UI 线程。
func (a *root) testConnForm() {
	if a.connTestBusy {
		return
	}
	p, err := a.connFormProfile()
	if err != nil {
		a.connTestMsg, a.connTestErr = err.Error(), true
		return
	}
	a.connTestMsg, a.connTestErr, a.connTestBusy = "测试中…", false, true
	probe := func() (string, bool) {
		start := time.Now()
		sess, err := engine.Open(engineProfile(p))
		if err != nil {
			return "失败：" + err.Error(), true
		}
		defer sess.Close()
		ctx, cancel := context.WithTimeout(context.Background(), connTestTimeout)
		defer cancel()
		if err := sess.Open(ctx); err != nil {
			return "失败：" + err.Error(), true
		}
		if _, err := sess.RunStatement(ctx, "SELECT 1"); err != nil {
			return "失败：" + err.Error(), true
		}
		return fmt.Sprintf("连接成功（%d ms）", time.Since(start).Milliseconds()), false
	}
	done := func(msg string, failed bool) {
		a.connTestBusy = false
		a.connTestMsg, a.connTestErr = msg, failed
		app.LogInfo("conn", "test", "kind", p.Kind, "name", p.Name, "ok", !failed, "msg", msg)
	}
	if a.win == nil {
		done(probe())
		return
	}
	win := a.win
	go func() {
		msg, failed := probe()
		win.Update(func() { done(msg, failed) })
	}()
}

// connFormSummary 把连接信息排成多行 "key: value"，供「复制」放进剪贴板。
// 和「导出连接」一致，不含密码。
func connFormSummary(p app.ConnProfile) string {
	lines := []string{
		"名称: " + p.Name,
		"类型: " + kindLabel(p.Kind),
	}
	if p.Kind == "sqlite" {
		lines = append(lines, "文件: "+p.Path)
		return strings.Join(lines, "\n")
	}
	lines = append(lines, fmt.Sprintf("主机: %s", p.Host), fmt.Sprintf("端口: %d", p.Port))
	if p.Database != "" {
		lines = append(lines, "数据库: "+p.Database)
	}
	if p.User != "" {
		lines = append(lines, "用户: "+p.User)
	}
	if p.Protocol != "" {
		lines = append(lines, "协议: "+p.Protocol)
	}
	if p.Params != "" {
		lines = append(lines, "参数: "+p.Params)
	}
	return strings.Join(lines, "\n")
}

// copyConnForm 把当前表单的连接信息写进剪贴板，并在弹窗里回一行提示。
func (a *root) copyConnForm(c *ui.Context) {
	p, err := a.connFormProfile()
	if err != nil {
		a.connTestMsg, a.connTestErr = err.Error(), true
		return
	}
	c.WriteClipboard(connFormSummary(p))
	a.connTestMsg, a.connTestErr = "已复制连接信息（不含密码）", false
}
