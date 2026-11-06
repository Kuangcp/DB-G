package ui

import (
	"context"
	"time"

	"db-g/app"
	"db-g/engine"
)

// probeConn 打开并立刻关闭连接，返回 "ok" 或失败原因（空 = 未知）。
func probeConn(p app.ConnProfile) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := engine.Open(engine.Profile{
		Kind: p.Kind, Name: p.Name, DSN: p.Path,
		Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database,
	})
	if err != nil {
		return "fail: " + err.Error()
	}
	if err := sess.Open(ctx); err != nil {
		return "fail: " + err.Error()
	}
	_ = sess.Close()
	return "ok"
}

// checkConnHealthSync 同步检查全部连接（测试用）。
func (a *root) checkConnHealthSync() {
	if a.store == nil {
		return
	}
	if a.connStatus == nil {
		a.connStatus = map[string]string{}
	}
	for _, p := range a.store.List() {
		a.connStatus[p.ID] = probeConn(p)
	}
}

// checkConnHealth 后台检查全部连接并更新状态点。
func (a *root) checkConnHealth() {
	if a.store == nil {
		return
	}
	list := a.store.List()
	apply := func(id, status string) {
		if a.connStatus == nil {
			a.connStatus = map[string]string{}
		}
		a.connStatus[id] = status
	}
	if a.win == nil {
		a.checkConnHealthSync()
		return
	}
	win := a.win
	for _, p := range list {
		go func(p app.ConnProfile) {
			status := probeConn(p)
			win.Update(func() { apply(p.ID, status) })
		}(p)
	}
}

// connStatusOf 返回连接健康状态（"" = 未知）。
func (a *root) connStatusOf(id string) string {
	if a.connStatus == nil {
		return ""
	}
	return a.connStatus[id]
}
