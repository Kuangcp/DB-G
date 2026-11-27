package ui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// slowHook 描述一次「慢建连」的测试桩行为，按 profile.DSN 取用。
type slowHook struct {
	started chan<- struct{}
	release <-chan struct{}
	opens   *int32
	fail    error
}

var (
	slowMu   sync.Mutex
	slowByID = map[string]*slowHook{}
)

// init 注册一个测试专用数据源：app 层只认内置 kind，所以借用 postgres 的名字，
// 把行为按 DSN 转到测试桩上（ui 测试不导入真实 postgres 驱动）。
func init() {
	engine.Register("postgres", engine.DriverMeta{Label: "PostgreSQL"}, func(p engine.Profile) (engine.Session, error) {
		slowMu.Lock()
		h := slowByID[p.DSN]
		slowMu.Unlock()
		if h == nil {
			return nil, errors.New("no slow hook for " + p.DSN)
		}
		if h.fail != nil {
			return nil, h.fail
		}
		return &slowSession{started: h.started, release: h.release, opens: h.opens}, nil
	})
}

func useSlowHook(t *testing.T, key string, h *slowHook) {
	t.Helper()
	slowMu.Lock()
	slowByID[key] = h
	slowMu.Unlock()
	t.Cleanup(func() {
		slowMu.Lock()
		delete(slowByID, key)
		slowMu.Unlock()
	})
}

// slowSession 的 Open 一直等 release，用来模拟网络不通时卡住的建连。
type slowSession struct {
	started chan<- struct{}
	release <-chan struct{}
	opens   *int32
}

func (s *slowSession) Open(ctx context.Context) error {
	if s.opens != nil {
		atomic.AddInt32(s.opens, 1)
	}
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *slowSession) Close() error { return nil }
func (s *slowSession) RunStatement(context.Context, string) (*engine.QueryResult, error) {
	return &engine.QueryResult{}, nil
}
func (s *slowSession) Cancel() bool              { return false }
func (s *slowSession) Caps() engine.Capabilities { return 0 }

// slowProfile 往连接库里加一个「慢」数据源，返回存好的档案。
func slowProfile(t *testing.T, a *root, name, key string) app.ConnProfile {
	t.Helper()
	err := a.store.Add(app.ConnProfile{
		Name: name, Kind: "postgres", Host: "slow.local", Port: 5432, Path: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range a.store.List() {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("profile %q not stored", name)
	return app.ConnProfile{}
}

// asyncWindow 让 root 走「有窗口」的异步路径；wait 等所有后台任务与回投完成。
func asyncWindow(t *testing.T, a *root) (wait func()) {
	t.Helper()
	var active atomic.Int32
	a.session.BgRun = func(work, done func()) {
		active.Add(1)
		go func() {
			defer active.Add(-1)
			work()
			done()
		}()
	}
	return func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if active.Load() == 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("background jobs did not finish")
	}
}

// 点开一个网络不通的数据源：建连必须在后台，侧栏先显示「连接中」，不卡住渲染。
func TestExpandConnDoesNotBlockWhileConnecting(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var opens int32
	useSlowHook(t, "slow-1", &slowHook{started: started, release: release, opens: &opens})

	a := newSidebarRoot(t)
	p := slowProfile(t, a, "慢库", "slow-1")
	wait := asyncWindow(t, a)

	begin := time.Now()
	a.expandConnAndTree(p)
	if waited := time.Since(begin); waited > 200*time.Millisecond {
		t.Fatalf("展开数据源阻塞了 %s，建连必须在后台", waited)
	}
	<-started
	if !a.connOpeningNow(p.ID) || a.connected(p.ID) {
		t.Fatalf("建连中状态不对: opening=%v connected=%v", a.connOpeningNow(p.ID), a.connected(p.ID))
	}

	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("连接中…") {
		t.Fatalf("侧栏应显示「连接中…」: %q", tt.Texts())
	}

	close(release)
	wait()
	if !a.connected(p.ID) || a.connOpeningNow(p.ID) {
		t.Fatalf("建连完成后状态不对: opening=%v connected=%v", a.connOpeningNow(p.ID), a.connected(p.ID))
	}
	tt.Frame()
	if tt.HasText("连接中…") {
		t.Fatalf("连上后不该再显示「连接中…」: %q", tt.Texts())
	}
	if got := atomic.LoadInt32(&opens); got != 1 {
		t.Fatalf("建连次数 = %d，应为 1", got)
	}
}

// 建连未完成时重复点同一个数据源：只发一次建连，等结果的回调都收到同一个结果。
func TestConnectAsyncDedupesInflightOpen(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var opens int32
	useSlowHook(t, "slow-2", &slowHook{started: started, release: release, opens: &opens})

	a := newSidebarRoot(t)
	p := slowProfile(t, a, "慢库2", "slow-2")
	wait := asyncWindow(t, a)

	calls := 0
	a.connectAsync(p, func(error) { calls++ })
	<-started
	a.connectAsync(p, func(error) { calls++ })
	a.connectAsync(p, nil)
	close(release)
	wait()
	if got := atomic.LoadInt32(&opens); got != 1 {
		t.Fatalf("并发点击建连了 %d 次，应为 1", got)
	}
	if calls != 2 {
		t.Fatalf("等待回调执行 %d 次，应为 2", calls)
	}
	if !a.connected(p.ID) {
		t.Fatal("建连完成后应装好会话")
	}
}

// 网络错误在后台返回：圆点转红、错误留给侧栏，不让事件回调卡住或 panic。
func TestConnectAsyncFailureMarksConnErr(t *testing.T) {
	useSlowHook(t, "slow-fail", &slowHook{fail: errors.New("dial tcp: network is unreachable")})

	a := newSidebarRoot(t)
	p := slowProfile(t, a, "坏库", "slow-fail")
	wait := asyncWindow(t, a)

	var gotErr error
	a.connectAsync(p, func(err error) { gotErr = err })
	wait()
	if gotErr == nil || a.connErrOf(p.ID) == "" {
		t.Fatalf("失败应回投错误: waiter=%v connErr=%q", gotErr, a.connErrOf(p.ID))
	}
	if a.connected(p.ID) || a.connOpeningNow(p.ID) {
		t.Fatal("失败后不该留下会话或建连中状态")
	}
}

// 执行前的兜底建连也走后台：headless 下同步完成，then 在会话装好之后才执行。
func TestEnsureConsoleConnAsyncConnectsBeforeRun(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "q")
	if err != nil {
		t.Fatal(err)
	}
	a.deferActivateConsole(rec.ID, "")
	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	if a.connected(prof.ID) {
		t.Fatal("渲染一帧不该偷偷建连")
	}

	ran := false
	a.ensureConsoleConnAsync(func() {
		if !a.connected(prof.ID) {
			t.Error("then 执行时会话必须已经装好")
		}
		ran = true
	})
	if !ran {
		t.Fatal("then 没有在 headless 同步路径里执行")
	}
	if !a.connected(prof.ID) {
		t.Fatal("兜底建连后应装好会话")
	}
}
