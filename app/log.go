package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

// Logger 是纯文本日志：一次启动一个文件，文件名按启动时刻的日期 + 当天下一个序号。
// 写入失败不影响调用方（日志不能把应用搞崩）。
type Logger struct {
	mu         sync.Mutex
	dir        string
	now        func() time.Time
	level      LogLevel
	alsoStderr bool

	f    *os.File
	path string
}

// DefaultLogDir 返回日志目录（与 connections.json 同级）。
func DefaultLogDir() string {
	return filepath.Join(filepath.Dir(DefaultStorePath()), "logs")
}

// NewLogger 按当前日期创建本次启动专属的日志文件：
// <dir>/<YYYY-MM>/<YYYY-MM-DD>_<N>.log。同一天再次启动时 N 递增，旧文件不覆盖。
func NewLogger(dir string) (*Logger, error) {
	l := &Logger{
		dir:   dir,
		now:   time.Now,
		level: LevelInfo,
		// stderr 同步便于 go run 排查；测试里用 DBG_LOG_STDERR=0 关掉噪音。
		alsoStderr: os.Getenv("DBG_LOG_STDERR") != "0",
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.openStartupLocked(l.now()); err != nil {
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

// Dir 返回日志根目录。
func (l *Logger) Dir() string { return l.dir }

// Path 返回本次启动的日志文件路径。
func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Log 写一行日志；kv 为交替的 key/value（奇数长度时忽略最后一个 key）。
func (l *Logger) Log(level LogLevel, tag, msg string, kv ...any) {
	l.mu.Lock()
	enabled := level >= l.level
	l.mu.Unlock()
	if !enabled {
		return
	}
	line := formatLogLine(l.now(), level, tag, msg, kv...)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	if _, err := l.f.WriteString(line); err != nil {
		// 写失败：放弃本行，不打扰调用方。
		return
	}
	if l.alsoStderr {
		_, _ = os.Stderr.WriteString(line)
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

// openStartupLocked 在当天的文件里挑下一个未使用的序号，用 O_EXCL 创建。
func (l *Logger) openStartupLocked(t time.Time) error {
	month := t.Format("2006-01")
	day := t.Format("2006-01-02")
	monthDir := filepath.Join(l.dir, month)
	if err := os.MkdirAll(monthDir, 0o700); err != nil {
		return err
	}
	prefix := day + "_"
	next := 0
	if entries, err := os.ReadDir(monthDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(e.Name(), prefix), ".log"))
			if err == nil && n >= next {
				next = n + 1
			}
		}
	}
	for part := next; part < next+100000; part++ {
		path := filepath.Join(monthDir, fmt.Sprintf("%s%d.log", prefix, part))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		l.f, l.path = f, path
		return nil
	}
	return fmt.Errorf("log: too many startup files for %s", day)
}

// formatLogLine 输出便于人读的纯文本：
// 2026-11-06 18:00:54.849 INFO  [conn] open name=prd_57 dsn=...
func formatLogLine(t time.Time, level LogLevel, tag, msg string, kv ...any) string {
	var b strings.Builder
	b.Grow(128)
	b.WriteString(t.Format("2006-01-02 15:04:05.000"))
	b.WriteByte(' ')
	lv := strings.ToUpper(level.String())
	b.WriteString(lv)
	for i := len(lv); i < 5; i++ {
		b.WriteByte(' ')
	}
	b.WriteString(" [")
	if tag == "" {
		tag = "-"
	}
	b.WriteString(tag)
	b.WriteString("] ")
	b.WriteString(msg)
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprint(kv[i]))
		b.WriteByte('=')
		b.WriteString(logValue(kv[i+1]))
	}
	b.WriteByte('\n')
	return b.String()
}

// logValue 让带空格/引号/换行的值保持单行可读。
func logValue(v any) string {
	s := fmt.Sprint(v)
	if s == "" || strings.ContainsAny(s, " \t\r\n\"'=") {
		return strconv.Quote(s)
	}
	return s
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
