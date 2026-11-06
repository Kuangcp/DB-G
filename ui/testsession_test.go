package ui

import (
	"testing"

	"db-g/engine"
)

// testSession 返回唯一那条已连接的会话。测试里通常只连一个数据源；
// 多连接并存的用例请直接用 sessionFor(connID)。
func (a *root) testSession(t *testing.T) engine.Session {
	t.Helper()
	if len(a.sessions) != 1 {
		ids := make([]string, 0, len(a.sessions))
		for id := range a.sessions {
			ids = append(ids, id)
		}
		t.Fatalf("测试只应有 1 个会话，实际 %d 个: %v", len(a.sessions), ids)
	}
	for _, s := range a.sessions {
		return s
	}
	return nil
}

// testConnID 返回唯一那条已连接的连接 id。
func (a *root) testConnID(t *testing.T) string {
	t.Helper()
	if len(a.sessions) != 1 {
		t.Fatalf("测试只应有 1 个会话，实际 %d 个", len(a.sessions))
	}
	for id := range a.sessions {
		return id
	}
	return ""
}

// testSetSession 直接往会话表里塞一个假会话（测试用，跳过真正的建连）。
func (a *root) testSetSession(connID string, sess engine.Session) {
	if a.sessions == nil {
		a.sessions = map[string]engine.Session{}
	}
	a.sessions[connID] = sess
	a.touchSession(connID)
}
