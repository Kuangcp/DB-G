package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// LogLevel 是日志级别；数值越大越严重。
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	}
	return "unknown"
}

const defaultMaxLogSize = 5 << 20 // 单文件 5MB 后轮转

// Logger 是 JSON Lines 日志：一天一文件、超过 maxSize 轮转、0600 权限。
// 写入失败不影响调用方（日志不能把应用搞崩）。
type Logger struct {
	mu         sync.Mutex
	dir        string
	now        func() time.Time
	level      LogLevel
	maxSize    int64
	alsoStderr bool

	f    *os.File
	path string
	day  string
	size int64
}

// DefaultLogDir 返回日志目录（与 connections.json 同级）。
func DefaultLogDir() string {
	return filepath.Join(filepath.Dir(DefaultStorePath()), "logs")
}

// NewLogger 在 dir 下打开（或创建）当天的日志文件。
func NewLogger(dir string) (*Logger, error) {
	l := &Logger{
		dir:     dir,
		now:     time.Now,
		level:   LevelInfo,
		maxSize: defaultMaxLogSize,
		// stderr 同步便于 go run 排查；测试里用 DBG_LOG_STDERR=0 关掉噪音。
		alsoStderr: os.Getenv("DBG_LOG_STDERR") != "0",
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.openLocked(l.now()); err != nil {
		return nil, err
	}
	return l, nil
}

// SetLevel 设置最低记录级别。
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Dir 返回日志目录。
func (l *Logger) Dir() string { return l.dir }

// Path 返回当前日志文件路径（可能随时间轮转变化）。
func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Log 写一行日志；kv 为交替的 key/value（奇数长度时忽略最后一个 key）。
func (l *Logger) Log(level LogLevel, tag, msg string, kv ...any) {
	l.mu.Lock()
	enabled := level >= l.level
	now := l.now()
	l.mu.Unlock()
	if !enabled {
		return
	}

	kvMap := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		kvMap[fmt.Sprint(kv[i])] = kv[i+1]
	}
	line, err := json.Marshal(map[string]any{
		"ts":    now.Format(time.RFC3339Nano),
		"level": level.String(),
		"tag":   tag,
		"msg":   msg,
		"kv":    kvMap,
	})
	if err != nil {
		return
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.rotateLocked(now, int64(len(line))); err != nil {
		return
	}
	n, werr := l.f.Write(line)
	l.size += int64(n)
	if werr != nil {
		// 写失败：放弃本行，不打扰调用方。
		return
	}
	if l.alsoStderr {
		_, _ = os.Stderr.Write(line)
	}
}

// Close 关闭当前文件。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// openLocked 打开某天的日志文件（追加）。
func (l *Logger) openLocked(t time.Time) error {
	day := t.Format("20060102")
	path := filepath.Join(l.dir, "app-"+day+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	size := int64(0)
	if fi, statErr := f.Stat(); statErr == nil {
		size = fi.Size()
	}
	l.f, l.path, l.day, l.size = f, path, day, size
	return nil
}

// rotateLocked 在跨天或超过上限时换文件。
func (l *Logger) rotateLocked(t time.Time, add int64) error {
	day := t.Format("20060102")
	if l.f != nil && day == l.day && l.size+add <= l.maxSize {
		return nil
	}
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	if day == l.day {
		// 同一天超限：当前文件改名 .1 后重开。
		rotated := filepath.Join(l.dir, "app-"+day+".1.log")
		_ = os.Remove(rotated)
		_ = os.Rename(l.path, rotated)
	}
	return l.openLocked(t)
}

var (
	defaultMu     sync.RWMutex
	defaultLogger *Logger
)

// InitLogs 初始化包级默认日志（供 ui 直接调用）。
func InitLogs(dir string) error {
	l, err := NewLogger(dir)
	if err != nil {
		return err
	}
	defaultMu.Lock()
	old := defaultLogger
	defaultLogger = l
	defaultMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// CloseLogs 关闭包级默认日志；之后再记日志是 no-op。
func CloseLogs() {
	defaultMu.Lock()
	l := defaultLogger
	defaultLogger = nil
	defaultMu.Unlock()
	if l != nil {
		_ = l.Close()
	}
}

// LogDir 返回当前生效的日志目录：已初始化时用实际目录，否则用默认目录。
func LogDir() string {
	defaultMu.RLock()
	l := defaultLogger
	defaultMu.RUnlock()
	if l != nil {
		return l.Dir()
	}
	return DefaultLogDir()
}

func logAt(level LogLevel, tag, msg string, kv ...any) {
	defaultMu.RLock()
	l := defaultLogger
	defaultMu.RUnlock()
	if l != nil {
		l.Log(level, tag, msg, kv...)
	}
}

// LogInfo/LogWarn/LogError 在未初始化时安全降级为 no-op。
func LogInfo(tag, msg string, kv ...any)  { logAt(LevelInfo, tag, msg, kv...) }
func LogWarn(tag, msg string, kv ...any)  { logAt(LevelWarn, tag, msg, kv...) }
func LogError(tag, msg string, kv ...any) { logAt(LevelError, tag, msg, kv...) }
func LogDebug(tag, msg string, kv ...any) { logAt(LevelDebug, tag, msg, kv...) }

var (
	dsnCredRE = regexp.MustCompile(`(^|[^A-Za-z0-9_.-])([A-Za-z0-9_.-]+):[^@/\s]*@`)
	dsnPassRE = regexp.MustCompile(`(?i)(password=|pwd=)([^ ]+)`)
)

// RedactDSN 脱敏连接串：保留用户名，抹掉密码。
func RedactDSN(s string) string {
	out := dsnCredRE.ReplaceAllString(s, "$1$2:***@")
	return dsnPassRE.ReplaceAllString(out, "$1***")
}
