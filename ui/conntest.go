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
	"db-g/locales"
)

// errPort 是端口填错的哨兵错误：调用方据此给中文提示。
func errPort() error { return errors.New(locales.PortMustBeNumber()) }

// errQueryTimeout 是查询超时填错的哨兵错误。
func errQueryTimeout() error { return errors.New(locales.ConnTimeoutInteger()) }

// connTestTimeout 是一次「测试连接」的上限。
const connTestTimeout = 5 * time.Second

// resetConnTest 清掉弹窗里那一行反馈（换类型、重新打开弹窗时）；递增轮次让在途结果作废。
func (a *root) resetConnTest() {
	a.connForm.TestSeq++
	a.connForm.TestMsg, a.connForm.TestErr, a.connForm.TestBusy = "", false, false
}

// cancelConnTest 取消正在进行的「测试连接」：递增轮次，在途 goroutine 的结果会被丢弃。
func (a *root) cancelConnTest() {
	if !a.connForm.TestBusy {
		return
	}
	a.connForm.TestSeq++
	a.connForm.TestBusy = false
	a.connForm.TestMsg, a.connForm.TestErr = locales.ConnTestCanceled(), false
}

// testConnForm 用表单当前内容试连一次：先按类型建会话，Open（内含 ping）再跑
// 一条 SELECT 1，确认账号真能查。有窗口时在后台跑，结果回投到 UI 线程。
func (a *root) testConnForm() {
	if a.connForm.TestBusy {
		return
	}
	p, err := a.connFormProfile()
	if err != nil {
		a.connForm.TestMsg, a.connForm.TestErr = err.Error(), true
		return
	}
	a.connForm.TestMsg, a.connForm.TestErr, a.connForm.TestBusy = locales.ConnTestRunning(), false, true
	a.connForm.TestSeq++
	seq := a.connForm.TestSeq
	probe := func() (string, bool) {
		start := time.Now()
		sess, err := engine.Open(engineProfile(p))
		if err != nil {
			return locales.ConnTestFailed(app.ErrorText(err)), true
		}
		defer sess.Close()
		ctx, cancel := context.WithTimeout(context.Background(), connTestTimeout)
		defer cancel()
		if err := sess.Open(ctx); err != nil {
			return locales.ConnTestFailed(app.ErrorText(err)), true
		}
		if _, err := sess.RunStatement(ctx, "SELECT 1"); err != nil {
			return locales.ConnTestFailed(app.ErrorText(err)), true
		}
		return locales.ConnTestOK(int(time.Since(start).Milliseconds())), false
	}
	done := func(msg string, failed bool) {
		if a.connForm.TestSeq != seq {
			return // 已取消或被新一轮取代
		}
		a.connForm.TestBusy = false
		a.connForm.TestMsg, a.connForm.TestErr = msg, failed
		app.LogInfo("conn", "test", "kind", p.Kind, "name", p.Name, "ok", !failed, "msg", msg)
	}
	var (
		msg    string
		failed bool
	)
	a.runBackground(func() {
		msg, failed = probe()
	}, func() {
		done(msg, failed)
	})
}

// connFormSummary 把连接信息排成多行 "key: value"，供「复制」放进剪贴板。
// 这里按用户要求带上明文密码；「导出连接」仍然不含密码。
func connFormSummary(p app.ConnProfile) string {
	lines := []string{
		locales.ConnInfoName() + ": " + p.Name,
		locales.ConnInfoKind() + ": " + kindLabel(p.Kind),
	}
	if p.Kind == "sqlite" {
		lines = append(lines, locales.ConnInfoFile()+": "+p.Path)
		if p.QueryTimeout > 0 {
			lines = append(lines, locales.ConnInfoTimeout(p.QueryTimeout))
		}
		return strings.Join(lines, "\n")
	}
	lines = append(lines, locales.ConnInfoHost()+": "+p.Host, locales.ConnInfoPort()+": "+fmt.Sprint(p.Port))
	if p.Database != "" {
		lines = append(lines, locales.ConnInfoDatabase()+": "+p.Database)
	}
	if p.User != "" {
		lines = append(lines, locales.ConnInfoUser()+": "+p.User)
	}
	if p.Password != "" {
		lines = append(lines, locales.ConnInfoPassword()+": "+p.Password)
	}
	if p.Protocol != "" {
		lines = append(lines, locales.ConnInfoProtocol()+": "+p.Protocol)
	}
	if p.Params != "" {
		lines = append(lines, locales.ConnInfoParams()+": "+p.Params)
	}
	if p.QueryTimeout > 0 {
		lines = append(lines, locales.ConnInfoTimeout(p.QueryTimeout))
	}
	return strings.Join(lines, "\n")
}

// copyConnForm 把当前表单的连接信息写进剪贴板，并在弹窗里回一行提示。
func (a *root) copyConnForm(c *ui.Context) {
	p, err := a.connFormProfile()
	if err != nil {
		a.connForm.TestMsg, a.connForm.TestErr = err.Error(), true
		return
	}
	c.WriteClipboard(connFormSummary(p))
	a.connForm.TestMsg, a.connForm.TestErr = locales.ConnInfoCopied(), false
}
